package devflow

import (
	"bufio"
	"fmt"
	"webtyp.com/command"
	gitmod "webtyp.com/git"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GoModHandler represents a parsed go.mod file and handles file events
type GoModHandler struct {
	Lines    []string // all lines of the file
	Modified bool     // track if changes were made

	// Handler fields
	rootDir         string
	watcher         FolderWatcher
	currentPaths    map[string]string // modulePath -> localPath
	log             func(messages ...any)
	knownReplaces   map[string]string
	OnSSRFileChange func(moduleDir string) // called when ssr.go changes in a watched module
}

// ReplaceEntry represents a local replace directive found in go.mod
type ReplaceEntry struct {
	ModulePath string // The module being replaced
	LocalPath  string // The local path replacement
}

// NewGoModHandler reads and parses a go.mod file or returns an empty handler if path is empty
func NewGoModHandler() *GoModHandler {
	return &GoModHandler{
		rootDir:       ".",
		currentPaths:  make(map[string]string),
		knownReplaces: make(map[string]string),
		log:           func(messages ...any) {},
	}
}

func (g *GoModHandler) load() error {
	gomodPath := filepath.Join(g.rootDir, "go.mod")
	content, err := os.ReadFile(gomodPath)
	if err != nil {
		return err
	}

	g.Lines = strings.Split(string(content), "\n")
	return nil
}

// RemoveReplace removes a replace directive for the given module.
// Local replace directives (target starting with "." or "/", e.g. "=> ./")
// are preserved: subpackages (tests/, cmd/, etc.) commonly use a self-referencing
// local replace to pull in the parent module without polluting the root go.mod,
// and that must survive dependent-module updates.
// Returns true if a replace was found and removed
func (m *GoModHandler) RemoveReplace(modulePath string) bool {
	// check if loaded
	if len(m.Lines) == 0 {
		if err := m.load(); err != nil {
			return false
		}
	}

	originalCount := len(m.Lines)
	var newLines []string
	inReplaceBlock := false
	removed := false

	for _, line := range m.Lines {
		trimmed := strings.TrimSpace(line)

		// Detect start/end of replace block
		if strings.HasPrefix(trimmed, "replace (") {
			inReplaceBlock = true
			newLines = append(newLines, line)
			continue
		}
		if inReplaceBlock && trimmed == ")" {
			inReplaceBlock = false
			// Check if we just emptied the block (last line was "replace (")
			if len(newLines) > 0 && strings.HasPrefix(strings.TrimSpace(newLines[len(newLines)-1]), "replace (") {
				newLines = newLines[:len(newLines)-1] // remove "replace ("
				removed = true
				continue
			}
			newLines = append(newLines, line)
			continue
		}

		// Check for the module in replace
		if strings.HasPrefix(trimmed, "replace ") || inReplaceBlock {
			// Extract module path from line
			mod, _ := parseReplaceLine(trimmed, inReplaceBlock)
			if mod == modulePath {
				if isLocalReplaceTarget(trimmed) {
					newLines = append(newLines, line)
					continue
				}
				removed = true
				continue // skip this line
			}
		}

		newLines = append(newLines, line)
	}

	if removed || len(newLines) != originalCount {
		m.Lines = newLines
		m.Modified = true
		return true
	}

	return false
}

func parseReplaceLine(line string, inBlock bool) (modPath, targetPath string) {
	s := line
	if !inBlock {
		s = strings.TrimPrefix(s, "replace ")
	}
	parts := strings.Split(s, "=>")
	if len(parts) != 2 {
		return "", ""
	}
	modPath = strings.TrimSpace(parts[0])
	targetPath = strings.TrimSpace(parts[1])
	if idx := strings.Index(targetPath, "//"); idx != -1 {
		targetPath = strings.TrimSpace(targetPath[:idx])
	}
	return modPath, targetPath
}

// isLocalReplaceTarget reports whether a replace directive line's target
// (the part after "=>") is a self-reference to the current directory (e.g.
// "=> ./"). Subpackages (tests/, cmd/, etc.) commonly declare their own
// go.mod with a replace like this to pull in the parent module locally
// without touching the root go.mod; other local paths (e.g. "../lib",
// pointing at an unrelated sibling checkout) are still eligible for removal.
func isLocalReplaceTarget(line string) bool {
	parts := strings.SplitN(line, "=>", 2)
	if len(parts) != 2 {
		return false
	}

	target := strings.TrimSpace(parts[1])
	if idx := strings.Index(target, "//"); idx != -1 {
		target = strings.TrimSpace(target[:idx])
	}

	return filepath.Clean(target) == "."
}

// GetReplacePaths returns absolute paths from local replace directives.
// Relative paths are resolved starting from the directory containing go.mod.
func (m *GoModHandler) GetReplacePaths() ([]ReplaceEntry, error) {
	// check if loaded
	if len(m.Lines) == 0 {
		if err := m.load(); err != nil {
			return nil, err
		}
	}

	var entries []ReplaceEntry
	inReplaceBlock := false
	rootDir := m.rootDir

	for _, line := range m.Lines {
		trimmed := strings.TrimSpace(line)

		// Detect start/end of replace block
		if strings.HasPrefix(trimmed, "replace (") {
			inReplaceBlock = true
			continue
		}
		if inReplaceBlock && trimmed == ")" {
			inReplaceBlock = false
			continue
		}

		if strings.HasPrefix(trimmed, "replace ") || inReplaceBlock {
			modPath, localPath := parseReplaceLine(trimmed, inReplaceBlock)
			if modPath == "" || localPath == "" {
				continue
			}

			// Check if localPath is actually a local path or a versioned module.
			// Local paths in go.mod MUST start with ./ or ../ or be absolute.
			isLocal := strings.HasPrefix(localPath, ".") || strings.HasPrefix(localPath, "/")
			if !isLocal {
				continue
			}

			// Resolve to absolute path
			absPath := localPath
			if !filepath.IsAbs(localPath) {
				absPath = filepath.Join(rootDir, localPath)
			}
			absPath, _ = filepath.Abs(absPath)

			entries = append(entries, ReplaceEntry{
				ModulePath: modPath,
				LocalPath:  absPath,
			})
		}
	}

	return entries, nil
}

// HasOtherReplaces returns true if there are replace directives
// other than the specified modules
func (m *GoModHandler) HasOtherReplaces(exceptModules ...string) bool {
	// check if loaded
	if len(m.Lines) == 0 {
		if err := m.load(); err != nil {
			return false
		}
	}

	inReplaceBlock := false
	for _, line := range m.Lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "replace (") {
			inReplaceBlock = true
			continue
		}
		if inReplaceBlock && trimmed == ")" {
			inReplaceBlock = false
			continue
		}

		if (strings.HasPrefix(trimmed, "replace ") || inReplaceBlock) && trimmed != "" {
			mod, _ := parseReplaceLine(trimmed, inReplaceBlock)
			isExcept := false
			for _, ex := range exceptModules {
				if ex != "" && mod == ex {
					isExcept = true
					break
				}
			}
			if isExcept {
				continue
			}
			return true
		}
	}
	return false
}

// EnsureReplace ensures that a replace directive exists for the given module path
// pointing to the given local path. Returns true if the file was modified.
func (m *GoModHandler) EnsureReplace(modulePath, localPath string) bool {
	// check if loaded
	if len(m.Lines) == 0 {
		if err := m.load(); err != nil {
		}
	}

	// Check if already exists
	found := false
	inReplaceBlock := false
	for _, line := range m.Lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "replace (") {
			inReplaceBlock = true
			continue
		}
		if inReplaceBlock && trimmed == ")" {
			inReplaceBlock = false
			continue
		}

		if strings.HasPrefix(trimmed, "replace ") || inReplaceBlock {
			mod, target := parseReplaceLine(trimmed, inReplaceBlock)
			if mod == modulePath && filepath.Clean(target) == filepath.Clean(localPath) {
				found = true
				break
			}
		}
	}

	if found {
		return false
	}

	// Add it. If there's a replace block, add it there. Otherwise, add a single-line replace.

	// Try to find a place to insert
	inserted := false
	var newLines []string
	for _, line := range m.Lines {
		newLines = append(newLines, line)
		if !inserted && strings.HasPrefix(strings.TrimSpace(line), "replace (") {
			newLines = append(newLines, "\t"+modulePath+" => "+localPath)
			inserted = true
		}
	}

	if !inserted {
		// Just append at the end
		newLines = append(newLines, "", "replace "+modulePath+" => "+localPath)
	}

	m.Lines = newLines
	m.Modified = true
	return true
}

// Save writes changes back to the file if modified
func (m *GoModHandler) Save() error {
	if !m.Modified {
		return nil
	}

	content := strings.Join(m.Lines, "\n")
	return os.WriteFile(filepath.Join(m.rootDir, "go.mod"), []byte(content), 0644)
}

// RunTidy executes 'go mod tidy' in the directory of the go.mod file
func (m *GoModHandler) RunTidy() error {
	dir := m.rootDir
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	_, err := cmd.CombinedOutput()
	return err
}

func (g *GoModHandler) SetRootDir(path string) {
	g.rootDir = path
}

func (g *GoModHandler) SetFolderWatcher(watcher FolderWatcher) {
	g.watcher = watcher
}

func (g *GoModHandler) SetLog(fn func(messages ...any)) {
	g.log = fn
}

func (g *GoModHandler) SetOnSSRFileChange(fn func(string)) {
	g.OnSSRFileChange = fn
}

func (m *GoModHandler) ObjectsToPublish(ctx gitmod.PublishContext) (gitmod.PublishAction, string) {
	m.SetRootDir(ctx.RepoDir)
	if m.HasOtherReplaces(ctx.ModulePaths...) {
		return gitmod.ActionDepsOnly, gitmod.ObjectionOtherReplaces
	}
	return gitmod.ActionNone, ""
}

func (g *GoModHandler) Name() string {
	return "GOMOD"
}

func (g *GoModHandler) MainInputFileRelativePath() string {
	return "go.mod"
}

func (g *GoModHandler) SupportedExtensions() []string {
	return []string{".mod"}
}

func (g *GoModHandler) UnobservedFiles() []string {
	return nil
}

// NewFileEvent handles changes to go.mod and ssr.go files in watched modules
func (g *GoModHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	// Relay ssr.go changes to registered callback for SSR asset hot reload
	if fileName == "ssr.go" && g.OnSSRFileChange != nil {
		g.OnSSRFileChange(filepath.Dir(filePath))
		return nil
	}

	// Only care about go.mod in the root directory
	if !strings.HasSuffix(filePath, "go.mod") {
		return nil
	}

	// Double check it's the root go.mod if rootDir is set
	if g.rootDir != "" {
		absFilePath, _ := filepath.Abs(filePath)
		absGoMod := filepath.Join(g.rootDir, "go.mod")
		if absFilePath != absGoMod {
			return nil
		}
	}

	// Refresh lines from file
	content, err := os.ReadFile(filePath)
	if err != nil {
		g.log("Error reading go.mod:", err)
		return err
	}
	g.Lines = strings.Split(string(content), "\n")
	g.Modified = false

	entries, err := g.GetReplacePaths()
	if err != nil {
		g.log("Error getting replace paths:", err)
		return err
	}

	g.reconcilePaths(entries)
	return nil
}

func (g *GoModHandler) reconcilePaths(entries []ReplaceEntry) {
	newMap := make(map[string]string)
	for _, entry := range entries {
		newMap[entry.ModulePath] = entry.LocalPath
	}

	// If watcher is not set, we can't do anything but update state
	if g.watcher == nil {
		g.currentPaths = newMap
		return
	}

	// Add new paths
	for mod, path := range newMap {
		if _, exists := g.currentPaths[mod]; !exists {
			g.log("GoModHandler: Watching external module:", path)
			if err := g.watcher.AddDirectoriesToWatch(path); err != nil {
				g.log("Frontend Error: Failed to watch external module:", path, err)
			}
		}
	}

	// Remove old paths
	for mod, path := range g.currentPaths {
		if _, exists := newMap[mod]; !exists {
			g.log("GoModHandler: Stop watching external module:", path)
			if err := g.watcher.RemoveDirectoriesFromWatcher(path); err != nil {
				g.log("Frontend Error: Failed to remove watch for external module:", path, err)
			}
		}
	}

	g.currentPaths = newMap
}

// GetModulePath gets full module path
func (g *Go) GetModulePath() (string, error) {
	file, err := os.Open(filepath.Join(g.rootDir, "go.mod"))
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module")), nil
		}
	}

	return "", fmt.Errorf("module directive not found in go.mod")
}

// ModExists checks if go.mod exists
func (g *Go) ModExists() bool {
	_, err := os.Stat(filepath.Join(g.rootDir, "go.mod"))
	return err == nil
}

// ModExistsInCurrentOrParent checks if go.mod exists in the rootDir or one directory up.
func (g *Go) ModExistsInCurrentOrParent() bool {
	// Check in rootDir
	if g.ModExists() {
		return true
	}
	// Check in parent
	parentDir := filepath.Dir(g.rootDir)
	if parentDir != g.rootDir { // Avoid infinite loop at system root
		_, err := os.Stat(filepath.Join(parentDir, "go.mod"))
		return err == nil
	}
	return false
}

// FindProjectRoot looks for go.mod in startDir or its immediate parent.
// Returns the absolute path to the directory containing go.mod, or an empty string and error if not found.
func FindProjectRoot(startDir string) (string, error) {
	absStart, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	// Check current directory
	if _, err := os.Stat(filepath.Join(absStart, "go.mod")); err == nil {
		return absStart, nil
	}

	// Check parent directory
	parent := filepath.Dir(absStart)
	if parent != absStart { // Avoid checking same dir if at root
		if _, err := os.Stat(filepath.Join(parent, "go.mod")); err == nil {
			return parent, nil
		}
	}

	return "", fmt.Errorf("could not find go.mod in %s or parent", absStart)
}

// Verify verifies go.mod integrity
func (g *Go) Verify() error {
	if !g.ModExists() {
		return fmt.Errorf("go.mod not found")
	}

	output, err := command.RunInDir(g.rootDir, "go", "mod", "verify")
	if err == nil {
		return nil
	}

	if msg, ok := ParseVerifyError(output); ok {
		return fmt.Errorf("%s", msg)
	}

	return err
}

// ParseVerifyError detects known go mod verify failure patterns and returns an actionable message.
func ParseVerifyError(output string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.Contains(line, "unknown revision"):
			mod := extractModuleRef(line)
			return fmt.Sprintf("module %s is not published — publish it or remove it from go.mod", mod), true
		case strings.Contains(line, "checksum mismatch"):
			mod := extractModuleRef(line)
			return fmt.Sprintf("checksum mismatch for %s — run `go clean -modcache` and retry", mod), true
		case strings.Contains(line, "missing go.sum entry"):
			return "go.sum is out of sync — run `go mod tidy`", true
		case strings.Contains(line, "updates to go.mod needed"):
			return "go.mod is out of sync — run `go mod tidy`", true
		}
	}
	return "", false
}

// extractModuleRef extracts "module@version" from a go error line.
// Example input: "go: github.com/foo/bar@v0.0.0: reading ...: unknown revision v0.0.0"
func extractModuleRef(line string) string {
	// Strip leading "go: "
	s := strings.TrimPrefix(line, "go: ")
	// Take everything up to the first ": "
	if idx := strings.Index(s, ": "); idx != -1 {
		return s[:idx]
	}
	return "unknown module"
}

// WaitForVersionAvailable waits for a module version to be available on Go proxy
func (g *Go) WaitForVersionAvailable(modulePath, version string) error {
	target := fmt.Sprintf("%s@%s", modulePath, version)

	maxRetries := g.retryAttempts
	if maxRetries < 1 {
		maxRetries = 1
	}

	delay := g.retryDelay
	if delay == 0 {
		delay = 5 * time.Second
	}

	for i := 0; i < maxRetries; i++ {
		_, err := command.Run("go", "list", "-m", target)
		if err == nil {
			return nil
		}
		if i < maxRetries-1 {
			fmt.Printf("⏳ Waiting for %s (attempt %d/%d)...\n", version, i+1, maxRetries)
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("version %s not available after %d attempts", version, maxRetries)
}

// UpdateDependents updates modules that depend on the current one
func (g *Go) UpdateDependents(modulePath, version, searchPath string) error {
	if searchPath == "" {
		searchPath = ".."
	}

	dependents, err := g.FindDependentModules(modulePath, searchPath)
	if err != nil {
		return err
	}

	if len(dependents) == 0 {
		return nil
	}

	if err := g.WaitForVersionAvailable(modulePath, version); err != nil {
		g.consoleOutput(fmt.Sprintf("⏳ %s", err))
		return nil
	}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5)

	g.consoleOutput(fmt.Sprintf("🚀 Updating %d dependents in parallel...", len(dependents)))

	for _, depDir := range dependents {
		wg.Add(1)
		go func(dir string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			// Every outcome — success, skip and failure alike — is streamed via
			// consoleOutput inside UpdateDependentModule, so one line per dependent
			// always reaches the terminal and the count above stays honest.
			_, _ = g.UpdateDependentModule(dir, []gitmod.DepBump{{ModulePath: modulePath, NewVersion: version}}, "")
		}(depDir)
	}

	wg.Wait()
	return nil
}

// FindDependentModules searches for modules that have modulePath as dependency.
// It excludes modules located inside the current project's root directory.
func (g *Go) FindDependentModules(modulePath, searchPath string) ([]string, error) {
	var dependents []string

	absRoot, _ := filepath.Abs(g.rootDir)

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue despite errors
		}

		// Only go.mod files
		if info.Name() != "go.mod" {
			return nil
		}

		dir := filepath.Dir(path)
		absDir, _ := filepath.Abs(dir)

		// Exclude internal submodules
		if strings.HasPrefix(absDir, absRoot+string(os.PathSeparator)) || absDir == absRoot {
			return nil
		}

		if g.HasDependency(path, modulePath) {
			dependents = append(dependents, dir)
		}

		return nil
	})

	return dependents, err
}

// HasDependency checks if a go.mod contains a specific dependency
func (g *Go) HasDependency(gomodPath, modulePath string) bool {
	content, err := os.ReadFile(gomodPath)
	if err != nil {
		return false
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Ignore the module declaration of the file itself
		if strings.HasPrefix(line, "module ") {
			if strings.TrimSpace(strings.TrimPrefix(line, "module")) == modulePath {
				return false
			}
			continue
		}

		fields := strings.Fields(line)
		for _, field := range fields {
			if field == modulePath {
				return true
			}
		}
	}

	return false
}

// UpdateModule updates a specific module to a new version
func (g *Go) UpdateModule(moduleDir, dependency, version string) error {
	originalDir, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(moduleDir); err != nil {
		return err
	}

	target := fmt.Sprintf("%s@%s", dependency, version)
	_, err = command.Run("go", "get", "-u", target)
	if err != nil {
		return fmt.Errorf("go get failed: %w", err)
	}

	_, err = command.Run("go", "mod", "tidy")
	if err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	return nil
}

// ModInit initializes a new go module
func (g *Go) ModInit(modulePath, targetDir string) error {
	originalDir, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(targetDir); err != nil {
		return err
	}

	_, err = command.Run("go", "mod", "init", modulePath)
	return err
}

// DetectGoExecutable returns the path to the go executable
func (g *Go) DetectGoExecutable() (string, error) {
	path, err := exec.LookPath("go")
	if err != nil {
		return "", err
	}
	return path, nil
}
