package devflow_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webtyp.com/devflow"
	gitmod "webtyp.com/git"
)

func TestCascadeFailures_FailedDependentIsRecorded(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "storage")
	bDir := testWriteModule(t, tmp, "b", "storage")

	g := newCascadeHandler(t, mainDir)
	ledgerFile := filepath.Join(tmp, "cascade_failures.json")
	g.SetCascadeLedger(ledgerFile)

	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		if node.ModulePath == "github.com/test/b" {
			return devflow.CascadeOutcome{}, fmt.Errorf("tests failed: expected nil got non-nil")
		}
		return devflow.CascadeOutcome{Status: devflow.CascadeStatusPublished, Version: "v9.9.9"}, nil
	})

	_ = g.RunCascade("github.com/test/storage", "v0.1.18", "", tmp)

	data, err := os.ReadFile(ledgerFile)
	if err != nil {
		t.Fatalf("failed to read ledger file: %v", err)
	}

	var entries []devflow.CascadeFailure
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(entries), entries)
	}

	absBDir, _ := filepath.Abs(bDir)
	e := entries[0]
	if filepath.Clean(e.Dir) != filepath.Clean(absBDir) {
		t.Errorf("expected Dir %s, got %s", absBDir, e.Dir)
	}
	if e.Module != "github.com/test/b" {
		t.Errorf("expected Module github.com/test/b, got %s", e.Module)
	}
	if e.Dependency != "github.com/test/storage" {
		t.Errorf("expected Dependency github.com/test/storage, got %s", e.Dependency)
	}
	if e.Version != "v0.1.18" {
		t.Errorf("expected Version v0.1.18, got %s", e.Version)
	}
	if e.Reason != "tests failed: expected nil got non-nil" {
		t.Errorf("expected Reason %q, got %q", "tests failed: expected nil got non-nil", e.Reason)
	}
	if e.Date != time.Now().Format("2006-01-02") {
		t.Errorf("expected Date %s, got %s", time.Now().Format("2006-01-02"), e.Date)
	}
}

func TestCascadeFailures_LaterSuccessClearsIt(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "storage")
	testWriteModule(t, tmp, "b", "storage")

	g := newCascadeHandler(t, mainDir)
	ledgerFile := filepath.Join(tmp, "cascade_failures.json")
	g.SetCascadeLedger(ledgerFile)

	// Step 1: fails
	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		return devflow.CascadeOutcome{}, fmt.Errorf("tests failed")
	})
	_ = g.RunCascade("github.com/test/storage", "v0.1.18", "", tmp)

	// Step 2: later success publishes
	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		return devflow.CascadeOutcome{Status: devflow.CascadeStatusPublished, Version: "v0.2.0"}, nil
	})
	_ = g.RunCascade("github.com/test/storage", "v0.1.18", "", tmp)

	data, err := os.ReadFile(ledgerFile)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read ledger: %v", err)
	}
	var entries []devflow.CascadeFailure
	if len(data) > 0 {
		_ = json.Unmarshal(data, &entries)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries in ledger after success, got %d: %+v", len(entries), entries)
	}
}

func TestCascadeFailures_NewerVersionReplaces(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "storage")
	testWriteModule(t, tmp, "b", "storage")

	g := newCascadeHandler(t, mainDir)
	ledgerFile := filepath.Join(tmp, "cascade_failures.json")
	g.SetCascadeLedger(ledgerFile)

	// First failure at v0.1.18
	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		return devflow.CascadeOutcome{}, fmt.Errorf("failed once")
	})
	_ = g.RunCascade("github.com/test/storage", "v0.1.18", "", tmp)

	// Second failure at v0.1.19
	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		return devflow.CascadeOutcome{}, fmt.Errorf("failed twice")
	})
	_ = g.RunCascade("github.com/test/storage", "v0.1.19", "", tmp)

	data, err := os.ReadFile(ledgerFile)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var entries []devflow.CascadeFailure
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Version != "v0.1.19" {
		t.Errorf("expected Version v0.1.19, got %s", entries[0].Version)
	}
	if entries[0].Reason != "failed twice" {
		t.Errorf("expected Reason 'failed twice', got %s", entries[0].Reason)
	}
}

func TestCascadeFailures_GotestShowsBehind(t *testing.T) {
	tmp := t.TempDir()
	bDir := testWriteModule(t, tmp, "b")
	goModContent := "module github.com/test/b\n\ngo 1.20\n\nrequire webtyp.com/storage v0.1.4\n"
	_ = os.WriteFile(filepath.Join(bDir, "go.mod"), []byte(goModContent), 0644)

	absBDir, _ := filepath.Abs(bDir)
	ledgerFile := filepath.Join(tmp, "cascade_failures.json")

	initialEntries := []devflow.CascadeFailure{
		{
			Dir:        absBDir,
			Module:     "github.com/test/b",
			Dependency: "webtyp.com/storage",
			Version:    "v0.1.18",
			Reason:     "broken null contract",
			Date:       "2026-10-12",
		},
	}
	data, _ := json.MarshalIndent(initialEntries, "", "  ")
	_ = os.WriteFile(ledgerFile, data, 0644)

	g := newCascadeHandler(t, bDir)
	g.SetCascadeLedger(ledgerFile)

	var lines []string
	g.SetConsoleOutput(func(s string) {
		lines = append(lines, s)
	})

	_, _ = g.Test(devflow.TestOptions{NoCache: true, SkipRace: true, Args: []string{"-list", ".*"}})

	fullOutput := strings.Join(lines, "\n")
	expected := "behind: webtyp.com/storage v0.1.4 → v0.1.18"
	if !strings.Contains(fullOutput, expected) {
		t.Errorf("expected output to contain %q, got output:\n%s", expected, fullOutput)
	}
	if !strings.Contains(fullOutput, "broken null contract") {
		t.Errorf("expected output to contain reason, got:\n%s", fullOutput)
	}
}

func TestCascadeFailures_GotestDropsWhenUpgraded(t *testing.T) {
	tmp := t.TempDir()
	bDir := testWriteModule(t, tmp, "b")
	goModContent := "module github.com/test/b\n\ngo 1.20\n\nrequire webtyp.com/storage v0.1.18\n"
	_ = os.WriteFile(filepath.Join(bDir, "go.mod"), []byte(goModContent), 0644)

	absBDir, _ := filepath.Abs(bDir)
	ledgerFile := filepath.Join(tmp, "cascade_failures.json")

	initialEntries := []devflow.CascadeFailure{
		{
			Dir:        absBDir,
			Module:     "github.com/test/b",
			Dependency: "webtyp.com/storage",
			Version:    "v0.1.18",
			Reason:     "broken null contract",
			Date:       "2026-10-12",
		},
	}
	data, _ := json.MarshalIndent(initialEntries, "", "  ")
	_ = os.WriteFile(ledgerFile, data, 0644)

	g := newCascadeHandler(t, bDir)
	g.SetCascadeLedger(ledgerFile)

	var lines []string
	g.SetConsoleOutput(func(s string) {
		lines = append(lines, s)
	})

	_, _ = g.Test(devflow.TestOptions{NoCache: true, SkipRace: true, Args: []string{"-list", ".*"}})

	fullOutput := strings.Join(lines, "\n")
	if strings.Contains(fullOutput, "behind: webtyp.com/storage") {
		t.Errorf("expected no behind warning for upgraded dependency, got:\n%s", fullOutput)
	}

	readData, err := os.ReadFile(ledgerFile)
	if err != nil {
		t.Fatalf("failed to read ledger: %v", err)
	}
	var entries []devflow.CascadeFailure
	_ = json.Unmarshal(readData, &entries)
	if len(entries) != 0 {
		t.Errorf("expected entry to be dropped, got %d entries: %+v", len(entries), entries)
	}
}

func TestCascadeFailures_GopushShowsBehind(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "main")
	bDir := testWriteModule(t, tmp, "b")
	goModContent := "module github.com/test/b\n\ngo 1.20\n\nrequire webtyp.com/storage v0.1.4\n"
	_ = os.WriteFile(filepath.Join(bDir, "go.mod"), []byte(goModContent), 0644)

	absBDir, _ := filepath.Abs(bDir)
	ledgerFile := filepath.Join(tmp, "cascade_failures.json")
	initialEntries := []devflow.CascadeFailure{
		{
			Dir:        absBDir,
			Module:     "github.com/test/b",
			Dependency: "webtyp.com/storage",
			Version:    "v0.1.18",
			Reason:     "broken null contract",
			Date:       "2026-10-12",
		},
	}
	data, _ := json.MarshalIndent(initialEntries, "", "  ")
	_ = os.WriteFile(ledgerFile, data, 0644)

	g := newCascadeHandler(t, mainDir)
	g.SetCascadeLedger(ledgerFile)

	var lines []string
	g.SetConsoleOutput(func(s string) {
		lines = append(lines, s)
	})

	_, _ = g.Push("feat: test", "", true, true, true, false, true, true, "")

	fullOutput := strings.Join(lines, "\n")
	if !strings.Contains(fullOutput, "Behind after failed cascades:") {
		t.Errorf("expected 'Behind after failed cascades:' in output, got:\n%s", fullOutput)
	}
	if !strings.Contains(fullOutput, "github.com/test/b needs webtyp.com/storage@v0.1.18") {
		t.Errorf("expected 'github.com/test/b needs webtyp.com/storage@v0.1.18' in output, got:\n%s", fullOutput)
	}
}
