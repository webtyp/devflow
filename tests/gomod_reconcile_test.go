package devflow_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

func TestGoModHandler_ReconcilePaths_BatchedLogs(t *testing.T) {
	t.Setenv("HOME", "/home/cesar")

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "project")
	os.MkdirAll(rootDir, 0755)

	watcher := &GoModMockWatcher{}
	handler := devflow.NewGoModHandler()
	handler.SetRootDir(rootDir)
	handler.SetFolderWatcher(watcher)

	var logCalls []string
	handler.SetLog(func(messages ...any) {
		var parts []string
		for _, m := range messages {
			parts = append(parts, fmt.Sprint(m))
		}
		logCalls = append(logCalls, strings.Join(parts, " "))
	})

	gomodPath := filepath.Join(rootDir, "go.mod")

	// 1. Initial 5 replace paths under /home/cesar/Dev/Project/webtyp
	content5 := "module testproject\n\ngo 1.25\n\n" +
		"replace webtyp.com/form => /home/cesar/Dev/Project/webtyp/form\n" +
		"replace webtyp.com/components => /home/cesar/Dev/Project/webtyp/components\n" +
		"replace webtyp.com/layout => /home/cesar/Dev/Project/webtyp/layout\n" +
		"replace webtyp.com/widget => /home/cesar/Dev/Project/webtyp/widget\n" +
		"replace webtyp.com/model => /home/cesar/Dev/Project/webtyp/model\n"

	os.WriteFile(gomodPath, []byte(content5), 0644)
	logCalls = nil

	if err := handler.NewFileEvent("go.mod", ".mod", gomodPath, "create"); err != nil {
		t.Fatalf("NewFileEvent failed: %v", err)
	}

	// Should emit exactly 1 call for 5 added paths, compacting into braces with ~
	if len(logCalls) != 1 {
		t.Fatalf("expected 1 log call for 5 added modules, got %d: %v", len(logCalls), logCalls)
	}
	if !strings.HasPrefix(logCalls[0], "Watching external modules: ") {
		t.Errorf("expected Watching prefix, got: %s", logCalls[0])
	}
	if strings.Contains(logCalls[0], "GoModHandler:") {
		t.Errorf("redundant GoModHandler prefix should be removed: %s", logCalls[0])
	}
	if !strings.Contains(logCalls[0], "~/Dev/Project/webtyp/{") {
		t.Errorf("expected compact path with ~, got: %s", logCalls[0])
	}

	// 2. No changes: 0 routes changed -> 0 log calls
	logCalls = nil
	if err := handler.NewFileEvent("go.mod", ".mod", gomodPath, "write"); err != nil {
		t.Fatalf("NewFileEvent failed: %v", err)
	}
	if len(logCalls) != 0 {
		t.Fatalf("expected 0 log calls for unchanged paths, got %d: %v", len(logCalls), logCalls)
	}

	// 3. Add 1 and remove 1 simultaneously -> exactly 2 calls
	content4plus1 := "module testproject\n\ngo 1.25\n\n" +
		"replace webtyp.com/form => /home/cesar/Dev/Project/webtyp/form\n" +
		"replace webtyp.com/components => /home/cesar/Dev/Project/webtyp/components\n" +
		"replace webtyp.com/layout => /home/cesar/Dev/Project/webtyp/layout\n" +
		"replace webtyp.com/widget => /home/cesar/Dev/Project/webtyp/widget\n" +
		"replace webtyp.com/storage => /home/cesar/Dev/Project/webtyp/storage\n" // removed model, added storage

	os.WriteFile(gomodPath, []byte(content4plus1), 0644)
	logCalls = nil

	if err := handler.NewFileEvent("go.mod", ".mod", gomodPath, "write"); err != nil {
		t.Fatalf("NewFileEvent failed: %v", err)
	}
	if len(logCalls) != 2 {
		t.Fatalf("expected 2 log calls (1 added, 1 removed), got %d: %v", len(logCalls), logCalls)
	}

	// 4. Remove all remaining 5 paths -> exactly 1 call
	contentEmpty := "module testproject\n\ngo 1.25\n\n"
	os.WriteFile(gomodPath, []byte(contentEmpty), 0644)
	logCalls = nil

	if err := handler.NewFileEvent("go.mod", ".mod", gomodPath, "write"); err != nil {
		t.Fatalf("NewFileEvent failed: %v", err)
	}
	if len(logCalls) != 1 {
		t.Fatalf("expected 1 log call for 5 removed modules, got %d: %v", len(logCalls), logCalls)
	}
	if !strings.HasPrefix(logCalls[0], "Stop watching external modules: ") {
		t.Errorf("expected Stop watching prefix, got: %s", logCalls[0])
	}
	if !strings.Contains(logCalls[0], "~/Dev/Project/webtyp/{") {
		t.Errorf("expected compact path with ~, got: %s", logCalls[0])
	}
}

func TestGoModHandler_SameSecondBurst_AccumulatesAndSharesOpID(t *testing.T) {
	t.Setenv("HOME", "/home/cesar")

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "project")
	os.MkdirAll(rootDir, 0755)

	watcher := &GoModMockWatcher{}
	handler := devflow.NewGoModHandler()
	handler.SetRootDir(rootDir)
	handler.SetFolderWatcher(watcher)

	var logCalls []string
	var opIDs []string
	handler.SetLog(func(messages ...any) {
		var parts []string
		for _, m := range messages {
			parts = append(parts, fmt.Sprint(m))
		}
		logCalls = append(logCalls, strings.Join(parts, " "))
		opIDs = append(opIDs, handler.GetLastOperationID())
	})

	gomodPath := filepath.Join(rootDir, "go.mod")

	// Initial state with 2 replaces
	os.WriteFile(gomodPath, []byte("module testproject\n\ngo 1.25\n\nreplace webtyp.com/model => /home/cesar/Dev/Project/webtyp/model\nreplace webtyp.com/input => /home/cesar/Dev/Project/webtyp/input\n"), 0644)
	_ = handler.NewFileEvent("go.mod", ".mod", gomodPath, "create")

	// Burst removal in the same second: first remove model, then remove input
	logCalls = nil
	opIDs = nil
	os.WriteFile(gomodPath, []byte("module testproject\n\ngo 1.25\n\nreplace webtyp.com/input => /home/cesar/Dev/Project/webtyp/input\n"), 0644)
	_ = handler.NewFileEvent("go.mod", ".mod", gomodPath, "write")

	os.WriteFile(gomodPath, []byte("module testproject\n\ngo 1.25\n"), 0644)
	_ = handler.NewFileEvent("go.mod", ".mod", gomodPath, "write")

	if len(logCalls) != 2 {
		t.Fatalf("expected 2 log calls, got %d: %v", len(logCalls), logCalls)
	}
	if opIDs[0] == "" || opIDs[0] != opIDs[1] {
		t.Errorf("expected same non-empty operationID across same-second burst, got %q vs %q", opIDs[0], opIDs[1])
	}
	if !strings.Contains(logCalls[1], "{model, input}") {
		t.Errorf("expected accumulated routes in second call, got: %s", logCalls[1])
	}
}
