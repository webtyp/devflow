package devflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gitmod "webtyp.com/git"
)

// CascadeFailure records a dependent module that failed to update during a cascade.
type CascadeFailure struct {
	Dir        string `json:"Dir"`        // absolute path of the dependent module (where its go.mod is)
	Module     string `json:"Module"`     // the dependent's module path
	Dependency string `json:"Dependency"` // the module that was published, e.g. webtyp.com/storage
	Version    string `json:"Version"`    // the version it failed to reach, e.g. v0.1.18
	Reason     string `json:"Reason"`     // first failure line, as reportFail printed it
	Date       string `json:"Date"`       // YYYY-MM-DD
}

var (
	cascadeLedgerMu   sync.Mutex
	cascadeLedgerPath string
)

// SetCascadeLedger injects the path for cascade failures ledger (used by tests).
func (g *Go) SetCascadeLedger(path string) {
	cascadeLedgerMu.Lock()
	defer cascadeLedgerMu.Unlock()
	cascadeLedgerPath = path
	if g != nil {
		g.cascadeLedger = path
	}
}

func (g *Go) getCascadeLedgerPathLocked() string {
	if g != nil && g.cascadeLedger != "" {
		return g.cascadeLedger
	}
	if cascadeLedgerPath != "" {
		return cascadeLedgerPath
	}
	xdg := os.Getenv("XDG_CACHE_HOME")
	if xdg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		xdg = filepath.Join(home, ".cache")
	}
	return filepath.Join(xdg, "devflow", "cascade_failures.json")
}

func loadCascadeFailuresFrom(p string) ([]CascadeFailure, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return []CascadeFailure{}, nil
		}
		return nil, err
	}
	var entries []CascadeFailure
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []CascadeFailure{}
	}
	return entries, nil
}

func saveCascadeFailuresTo(p string, entries []CascadeFailure) error {
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	if entries == nil {
		entries = []CascadeFailure{}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

func (g *Go) recordCascadeFailure(entry CascadeFailure) error {
	cascadeLedgerMu.Lock()
	defer cascadeLedgerMu.Unlock()

	p := g.getCascadeLedgerPathLocked()
	entries, _ := loadCascadeFailuresFrom(p)
	absEntryDir, _ := filepath.Abs(entry.Dir)
	entry.Dir = absEntryDir

	found := false
	for i, e := range entries {
		eDir, _ := filepath.Abs(e.Dir)
		if filepath.Clean(eDir) == filepath.Clean(absEntryDir) && e.Dependency == entry.Dependency {
			entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, entry)
	}
	return saveCascadeFailuresTo(p, entries)
}

func (g *Go) clearCascadeFailure(dir, dependency string) error {
	cascadeLedgerMu.Lock()
	defer cascadeLedgerMu.Unlock()

	p := g.getCascadeLedgerPathLocked()
	entries, _ := loadCascadeFailuresFrom(p)
	if len(entries) == 0 {
		return nil
	}
	absTargetDir, _ := filepath.Abs(dir)
	cleaned := entries[:0]
	for _, e := range entries {
		eDir, _ := filepath.Abs(e.Dir)
		if filepath.Clean(eDir) == filepath.Clean(absTargetDir) && e.Dependency == dependency {
			continue
		}
		cleaned = append(cleaned, e)
	}
	return saveCascadeFailuresTo(p, cleaned)
}

func (g *Go) recordCascadeFailuresForNode(node CascadeNode, bumps []gitmod.DepBump, err error) {
	absDir, errDir := filepath.Abs(node.Dir)
	if errDir != nil {
		absDir = node.Dir
	}
	date := time.Now().Format("2006-01-02")
	reason := firstLineOfError(err)
	for _, b := range bumps {
		_ = g.recordCascadeFailure(CascadeFailure{
			Dir:        absDir,
			Module:     node.ModulePath,
			Dependency: b.ModulePath,
			Version:    b.NewVersion,
			Reason:     reason,
			Date:       date,
		})
	}
}

func (g *Go) clearCascadeFailuresForNode(node CascadeNode, bumps []gitmod.DepBump) {
	absDir, errDir := filepath.Abs(node.Dir)
	if errDir != nil {
		absDir = node.Dir
	}
	for _, b := range bumps {
		_ = g.clearCascadeFailure(absDir, b.ModulePath)
	}
}

func firstLineOfError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if idx := strings.Index(msg, "\n"); idx != -1 {
		msg = msg[:idx]
	}
	return strings.TrimSpace(msg)
}

func getRequiredVersionFromGoMod(goModPath, dependency string) string {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	inBlock := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "require (" {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}
		if inBlock {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == dependency {
				return fields[1]
			}
			continue
		}
		if strings.HasPrefix(line, "require ") {
			fields := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(fields) >= 2 && fields[0] == dependency {
				return fields[1]
			}
		}
	}
	return ""
}

// printBehindCascadeFailuresForDir checks the ledger for entries matching targetDir.
// If the local go.mod already requires >= Version, the entry is dropped from the ledger.
// Otherwise, it prints: ⚠️ behind: <dependency> <current> → <version> failed the cascade on <date>: <reason>
func (g *Go) printBehindCascadeFailuresForDir(targetDir string) {
	cascadeLedgerMu.Lock()
	defer cascadeLedgerMu.Unlock()

	p := g.getCascadeLedgerPathLocked()
	entries, err := loadCascadeFailuresFrom(p)
	if err != nil || len(entries) == 0 {
		return
	}

	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		absTargetDir = targetDir
	}

	goModPath := filepath.Join(absTargetDir, "go.mod")
	var kept []CascadeFailure
	modified := false

	for _, e := range entries {
		eDir, _ := filepath.Abs(e.Dir)
		if filepath.Clean(eDir) != filepath.Clean(absTargetDir) {
			kept = append(kept, e)
			continue
		}

		// It matches our directory
		currentVer := getRequiredVersionFromGoMod(goModPath, e.Dependency)
		if currentVer != "" && gitmod.CompareVersions(currentVer, e.Version) >= 0 {
			// Upgraded! Drop the entry
			modified = true
			continue
		}

		// Still behind
		kept = append(kept, e)
		if currentVer == "" {
			currentVer = "unknown"
		}
		if g.consoleOutput != nil {
			g.consoleOutput(fmt.Sprintf("⚠️ behind: %s %s → %s failed the cascade on %s: %s",
				e.Dependency, currentVer, e.Version, e.Date, e.Reason))
		}
	}

	if modified {
		_ = saveCascadeFailuresTo(p, kept)
	}
}

// printAllBehindCascadeFailures prints open cascade failure entries across all repos.
func (g *Go) printAllBehindCascadeFailures() {
	cascadeLedgerMu.Lock()
	defer cascadeLedgerMu.Unlock()

	p := g.getCascadeLedgerPathLocked()
	entries, err := loadCascadeFailuresFrom(p)
	if err != nil || len(entries) == 0 {
		return
	}

	var kept []CascadeFailure
	modified := false

	for _, e := range entries {
		goModPath := filepath.Join(e.Dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			currentVer := getRequiredVersionFromGoMod(goModPath, e.Dependency)
			if currentVer != "" && gitmod.CompareVersions(currentVer, e.Version) >= 0 {
				modified = true
				continue
			}
		}
		kept = append(kept, e)
	}

	if modified {
		_ = saveCascadeFailuresTo(p, kept)
	}

	if len(kept) == 0 {
		return
	}

	if g.consoleOutput != nil {
		g.consoleOutput("Behind after failed cascades:")
		for _, e := range kept {
			g.consoleOutput(fmt.Sprintf("  %s needs %s@%s", e.Module, e.Dependency, e.Version))
		}
	}
}
