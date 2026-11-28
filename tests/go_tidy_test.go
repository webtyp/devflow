package devflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"webtyp.com/devflow"
)

func TestNeedsGoModTidy(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{"go: updates to go.mod needed; to update it:\n\tgo mod tidy", true},
		{"missing go.sum entry for module providing package webtyp.com/html", true},
		{"undefined: Foo", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := devflow.NeedsGoModTidy(tc.out); got != tc.want {
			t.Errorf("NeedsGoModTidy(%q) = %v, want %v", tc.out, got, tc.want)
		}
	}
}

func TestGoModHandler_TidyModuleIfNeeded_RootFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/tidytest\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var logs []string
	gm := devflow.NewGoModHandler()
	gm.SetRootDir(dir)
	gm.SetLog(func(args ...any) {
		var parts []string
		for _, a := range args {
			if s, ok := a.(string); ok {
				parts = append(parts, s)
			}
		}
		logs = append(logs, strings.Join(parts, " "))
	})

	ran, err := gm.TidyModuleIfNeeded("", "undefined: Foo")
	if err != nil || ran {
		t.Fatalf("unrelated error: ran=%v err=%v", ran, err)
	}

	ran, err = gm.TidyModuleIfNeeded("", "go: updates to go.mod needed; to update it:\n\tgo mod tidy")
	if err != nil || !ran {
		t.Fatalf("expected tidy to run: ran=%v err=%v", ran, err)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "ran go mod tidy in") {
		t.Fatalf("expected tidy log line, got %v", logs)
	}
}

func TestGoModHandler_TidyModuleIfNeeded_ReplaceSubdir(t *testing.T) {
	appDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Separate local replace module with a nested package directory.
	replaceMod := t.TempDir()
	nestedPkg := filepath.Join(replaceMod, "ui", "components")
	if err := os.MkdirAll(nestedPkg, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replaceMod, "go.mod"), []byte("module example.com/replaced\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedPkg, "css.go"), []byte("package components\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var logs []string
	gm := devflow.NewGoModHandler()
	gm.SetRootDir(appDir)
	gm.SetLog(func(args ...any) {
		var parts []string
		for _, a := range args {
			if s, ok := a.(string); ok {
				parts = append(parts, s)
			}
		}
		mu.Lock()
		logs = append(logs, strings.Join(parts, " "))
		mu.Unlock()
	})

	ran, err := gm.TidyModuleIfNeeded(nestedPkg, "missing go.sum entry for module providing package example.com/x")
	if err != nil || !ran {
		t.Fatalf("expected tidy in replace module: ran=%v err=%v", ran, err)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], filepath.Base(replaceMod)) {
		t.Fatalf("expected log to reference replace module root %q, got %v", replaceMod, logs)
	}
	if strings.Contains(logs[0], filepath.Base(appDir)) {
		t.Fatalf("tidy ran in appDir instead of replace module: %v", logs)
	}

	// Concurrent calls targeting the same module must serialize cleanly.
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := gm.TidyModuleIfNeeded(nestedPkg, "missing go.sum entry")
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent TidyModuleIfNeeded failed: %v", err)
	}
}
