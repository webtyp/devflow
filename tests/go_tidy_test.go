package devflow_test

import (
	"os"
	"path/filepath"
	"strings"
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

func TestGoModHandler_TidyIfNeeded(t *testing.T) {
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

	ran, err := gm.TidyIfNeeded("undefined: Foo")
	if err != nil || ran {
		t.Fatalf("unrelated error: ran=%v err=%v", ran, err)
	}

	ran, err = gm.TidyIfNeeded("go: updates to go.mod needed; to update it:\n\tgo mod tidy")
	if err != nil || !ran {
		t.Fatalf("expected tidy to run: ran=%v err=%v", ran, err)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "ran go mod tidy in") {
		t.Fatalf("expected tidy log line, got %v", logs)
	}
}
