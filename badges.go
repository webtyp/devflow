package devflow

import (
	"bufio"
	"bytes"
	"fmt"
	"webtyp.com/command"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const DevFlowRepository = "webtyp.com/devflow"

// Badges is responsible for creating and managing a collection of badges.
// It handles parsing input arguments, generating the SVG image, and preparing
// the necessary markdown to embed the badges in a file.
type Badges struct {
	args       []string
	rootDir    string
	outputFile string
	readmeFile string
	// stored initialization/parsing error; methods must check this
	err error
	// Go handler
	goH *Go
	// configuration (moved from package-level constants)
	svgHeight    int
	badgeHeight  int
	fontSize     int
	labelPadding int
	valuePadding int
	badgeSpacing int
	labelBg      string
	// text used in the svg comment/header
	svgInfo string
	log     func(...any)
}

// NewBadges creates and initializes a new Badges handler.
func NewBadges(args ...string) *Badges {
	// Create handler with defaults
	h := &Badges{
		rootDir:      "",
		outputFile:   "docs/img/badges.svg",
		svgHeight:    20,
		badgeHeight:  20,
		fontSize:     11,
		labelPadding: 6,
		valuePadding: 6,
		badgeSpacing: 5,
		labelBg:      "#6c757d",
		svgInfo:      DevFlowRepository,
		log:          func(...any) {},
	}

	// Create a default Go handler.
	g, _ := NewGo(nil)
	h.goH = g

	// If the first argument is a directory, treat it as the current working directory.
	if len(args) > 0 {
		if fi, err := os.Stat(args[0]); err == nil && fi.IsDir() {
			gitPath := filepath.Join(args[0], ".git")
			if _, serr := os.Stat(gitPath); os.IsNotExist(serr) {
				h.err = fmt.Errorf("Git repository not found")
				return h
			}
			// remove the injected current-dir arg
			args = args[1:]
		}
	}

	if len(args) == 0 {
		h.err = fmt.Errorf("no badges specified, usage: badges.sh \"label:value:color\" \"label:value:color\"")
		return h
	}

	// assign args and process args to detect special commands (output_svgfile: and readmefile:)
	h.args = args
	// defaults
	h.readmeFile = "README.md"
	for _, a := range args {
		if strings.HasPrefix(a, "output_svgfile:") {
			h.outputFile = a[len("output_svgfile:"):]
			continue
		}
		if strings.HasPrefix(a, "readmefile:") {
			h.readmeFile = a[len("readmefile:"):]
			continue
		}
	}

	return h
}

// SetLog sets the logger function
func (h *Badges) SetLog(fn func(...any)) {
	if fn != nil {
		h.log = fn
		if h.goH != nil {
			h.goH.SetLog(fn)
		}
	}
}

// SetRootDir sets the root directory for badge operations
func (h *Badges) SetRootDir(dir string) {
	h.rootDir = dir
	if h.goH != nil {
		h.goH.SetRootDir(dir)
	}
}

func (h *Badges) getRootDir() string {
	if h.rootDir == "" {
		return "."
	}
	return h.rootDir
}

// BuildBadges generates the SVG image, writes it to the specified output file,
// and returns a slice of strings intended for updating a markdown file.
func (h *Badges) BuildBadges() ([]string, error) {
	if h.err != nil {
		return nil, h.err
	}

	// First pass: collect parse errors/warnings like the bash implementation
	var errorMessages []string
	generatedBadgesCount := 0
	for _, p := range h.args {
		// skip special commands
		if strings.HasPrefix(p, "output_svgfile:") || strings.HasPrefix(p, "readmefile:") {
			continue
		}
		_, perr := parseBadge(p)
		if perr != nil {
			// special command sentinel
			if strings.Contains(perr.Error(), "special command") {
				continue
			}
			// Normalize messages to match the bash tests expectations
			if strings.Contains(perr.Error(), "invalid badge format") {
				errorMessages = append(errorMessages, fmt.Sprintf("Error: Invalid badge format: %s", p))
			} else if strings.Contains(perr.Error(), "empty fields in badge") {
				errorMessages = append(errorMessages, fmt.Sprintf("Error: Empty fields in badge: %s", p))
			} else {
				errorMessages = append(errorMessages, fmt.Sprintf("Error: %s", perr.Error()))
			}
			continue
		}
		generatedBadgesCount++
	}

	// Generate SVG
	svgBytes, _, genErr := h.GenerateSVG()

	// Print accumulated error messages
	if len(errorMessages) > 0 {
		for _, m := range errorMessages {
			h.log(m)
		}
	}

	if genErr != nil {
		// No valid badges case should match bash: print explicit message
		if generatedBadgesCount == 0 {
			h.log("Error: No valid badges to generate")
		}
		return nil, genErr
	}

	// ensure directory exists when writing file
	outputPath := h.outputFile
	if !filepath.IsAbs(outputPath) && h.rootDir != "" {
		outputPath = filepath.Join(h.rootDir, h.outputFile)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	// Check if file exists and content is the same (don't rewrite if identical)
	shouldWrite := true
	if existing, err := os.ReadFile(outputPath); err == nil {
		if bytes.Equal(existing, svgBytes) {
			shouldWrite = false
		}
	}

	if shouldWrite {
		if err := os.WriteFile(outputPath, svgBytes, 0o644); err != nil {
			return nil, fmt.Errorf("write svg file: %w", err)
		}
	}

	// Update README with badge img line
	if err := h.UpdateReadme(); err != nil {
		return nil, fmt.Errorf("update readme: %w", err)
	}

	return nil, nil
}

// UpdateReadme updates the README file with the badge image line.
// Detects and migrates old START_SECTION/END_SECTION markers,
// replaces existing badge img lines, or inserts a new one.
func (h *Badges) UpdateReadme() error {
	if h.err != nil {
		return h.err
	}

	readmePath := h.readmeFile
	if !filepath.IsAbs(readmePath) && h.rootDir != "" {
		readmePath = filepath.Join(h.rootDir, h.readmeFile)
	}
	// Read the README file
	content, err := os.ReadFile(readmePath)
	if err != nil {
		// File doesn't exist yet, create it with just the badge
		newContent := h.BadgeMarkdown() + "\n"
		return os.WriteFile(readmePath, []byte(newContent), 0o644)
	}

	currentContent := string(content)
	lines := strings.Split(currentContent, "\n")

	badgeLine := h.BadgeMarkdown()
	sectionStart := "<!-- START_SECTION:BADGES_SECTION -->"
	sectionEnd := "<!-- END_SECTION:BADGES_SECTION -->"

	// First pass: find and remove old section markers
	var newLines []string
	markerFoundAt := -1
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		// Check for start marker
		if trimmed == sectionStart {
			if markerFoundAt == -1 {
				markerFoundAt = len(newLines)
			}
			// Skip lines until end marker (including content between them)
			for i < len(lines)-1 {
				i++
				if strings.TrimSpace(lines[i]) == sectionEnd {
					break
				}
			}
			// Don't add any lines between markers - they'll be replaced by badge
			continue
		}
		newLines = append(newLines, lines[i])
	}

	// If markers were found, insert badge line at that position
	if markerFoundAt >= 0 {
		newLines = append(newLines[:markerFoundAt], append([]string{badgeLine}, newLines[markerFoundAt:]...)...)
	} else {
		// No markers found - check for standalone badge img line to replace
		foundBadgeAt := -1
		for i, line := range newLines {
			// Match standalone img lines (not inside <a> tags)
			if strings.TrimSpace(line) == badgeLine {
				foundBadgeAt = i
				break
			}
			// Also match old format img lines that need updating
			if matched, _ := regexp.MatchString(`^\s*<img src=".*badges\.svg">\s*$`, line); matched {
				foundBadgeAt = i
				break
			}
		}

		if foundBadgeAt >= 0 {
			// Replace existing badge line
			newLines[foundBadgeAt] = badgeLine
		} else {
			// No badge line found - check if we need to insert one
			// Insert after line 1 (assuming title/header) if not already present
			if len(newLines) > 1 {
				newLines = append(newLines[:1], append([]string{badgeLine}, newLines[1:]...)...)
			} else {
				newLines = append(newLines, badgeLine)
			}
		}
	}

	newContent := strings.Join(newLines, "\n")

	// Only write if content changed
	if newContent == currentContent {
		h.log("Badge line already up to date")
		return nil
	}

	if err := os.WriteFile(readmePath, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("write readme: %w", err)
	}

	h.log("Updated badges in", h.readmeFile)
	return nil
}

// OutputFile returns the configured path for the output SVG file.
func (h *Badges) OutputFile() string {
	if h.err != nil {
		return ""
	}
	return h.outputFile
}

// ReadmeFile returns the configured path for the markdown file to be updated.
func (h *Badges) ReadmeFile() string {
	if h.err != nil {
		return ""
	}
	return h.readmeFile
}

// BadgeMarkdown generates the markdown snippet for embedding the badge image.
func (h *Badges) BadgeMarkdown() string {
	if h.err != nil {
		return ""
	}

	return fmt.Sprintf(`<img src="%s">`, h.outputFile)
}

// Err returns any error that occurred during the initialization or processing
func (h *Badges) Err() error {
	return h.err
}

// GoHandler returns the internal Go handler
func (h *Badges) GoHandler() *Go {
	return h.goH
}

func GetGoVersion() string {
	out, err := command.Run("go", "version")
	if err != nil {
		return "unknown"
	}
	parts := strings.Fields(out)
	if len(parts) >= 3 {
		return strings.TrimPrefix(parts[2], "go")
	}
	return "unknown"
}

func GetBadgeColor(typ, value string) string {
	switch typ {
	case "license", "go":
		return "#007acc"
	case "tests":
		if value == "Passing" {
			return "#4c1"
		}
		return "#e05d44"
	case "coverage":
		val, _ := strconv.ParseFloat(value, 64)
		if val >= 80 {
			return "#4c1"
		} else if val >= 60 {
			return "#dfb317"
		} else if val > 0 {
			return "#fe7d37"
		}
		return "#e05d44"
	case "race":
		if value == "Clean" {
			return "#4c1"
		}
		return "#e05d44"
	case "vet":
		if value == "OK" {
			return "#4c1"
		}
		return "#e05d44"
	}
	return "#007acc"
}

func getModuleName(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimPrefix(line, "module "), nil
		}
	}
	return "", fmt.Errorf("module name not found in go.mod")
}

func checkFileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// UpdateBadges generates badge SVG and updates the README using the provided values.
func (h *Badges) UpdateBadges(readmeFile, licenseType, goVer, testStatus, coveragePercent, raceStatus, vetStatus string, quiet bool) error {
	return h.updateBadges(readmeFile, licenseType, goVer, testStatus, coveragePercent, raceStatus, vetStatus, quiet)
}

func (h *Badges) updateBadges(readmeFile, licenseType, goVer, testStatus, coveragePercent, raceStatus, vetStatus string, quiet bool) error {
	// Colors
	licenseColor := GetBadgeColor("license", licenseType)
	goColor := GetBadgeColor("go", goVer)
	testColor := GetBadgeColor("tests", testStatus)
	coverageColor := GetBadgeColor("coverage", coveragePercent)
	raceColor := GetBadgeColor("race", raceStatus)
	vetColor := GetBadgeColor("vet", vetStatus)

	badgeArgs := []string{
		"readmefile:" + readmeFile,
		fmt.Sprintf("License:%s:%s", licenseType, licenseColor),
		fmt.Sprintf("Go:%s:%s", goVer, goColor),
		fmt.Sprintf("Tests:%s:%s", testStatus, testColor),
		fmt.Sprintf("Coverage:%s%%:%s", coveragePercent, coverageColor),
		fmt.Sprintf("Race:%s:%s", raceStatus, raceColor),
		fmt.Sprintf("Vet:%s:%s", vetStatus, vetColor),
	}

	bh := NewBadges(badgeArgs...)
	bh.SetRootDir(h.rootDir)
	bh.SetLog(h.log)
	if _, err := bh.BuildBadges(); err != nil {
		return fmt.Errorf("error building badges: %w", err)
	}

	if !quiet {
		h.log(fmt.Sprintf("Updated badges in %s", readmeFile))
	}

	return nil
}
