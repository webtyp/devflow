package devflow_test

import "webtyp.com/devflow"

import (
	gitmod "webtyp.com/git"
	"os"
	"path/filepath"
	"testing"
)

type GoModMockWatcher struct {
	watchedPaths []string
}

func (m *GoModMockWatcher) AddDirectoriesToWatch(paths ...string) error {
	m.watchedPaths = append(m.watchedPaths, paths...)
	return nil
}

func (m *GoModMockWatcher) RemoveDirectoriesFromWatcher(paths ...string) error {
	// Simple mock removing from the slice
	for _, path := range paths {
		for i, p := range m.watchedPaths {
			if p == path {
				m.watchedPaths = append(m.watchedPaths[:i], m.watchedPaths[i+1:]...)
				break
			}
		}
	}
	return nil
}

func TestGoModHandler(t *testing.T) {
	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "project")
	os.MkdirAll(rootDir, 0755)

	externalLibDir := filepath.Join(tmpDir, "external_lib")
	os.MkdirAll(externalLibDir, 0755)

	// Create go.mod with replace
	gomodPath := filepath.Join(rootDir, "go.mod")
	content := "module testproject\n\ngo 1.25\n\nreplace github.com/test/lib => ../external_lib\n"
	os.WriteFile(gomodPath, []byte(content), 0644)

	watcher := &GoModMockWatcher{}
	handler := devflow.NewGoModHandler()
	handler.SetRootDir(rootDir)
	handler.SetFolderWatcher(watcher)

	t.Run("InitializeRegistration_DetectsReplace", func(t *testing.T) {
		// NewFileEvent called with "create" during InitialRegistration
		err := handler.NewFileEvent("go.mod", ".mod", gomodPath, "create")
		if err != nil {
			t.Fatalf("NewFileEvent failed: %v", err)
		}

		absExternal, _ := filepath.Abs(externalLibDir)
		found := false
		for _, v := range watcher.watchedPaths {
			if v == absExternal {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Expected path %s to be watched, got %v", absExternal, watcher.watchedPaths)
		}
	})

	t.Run("DetectsChangesAtRuntime", func(t *testing.T) {

		// Update go.mod with another replace
		externalLib2Dir := filepath.Join(tmpDir, "external_lib2")
		os.MkdirAll(externalLib2Dir, 0755)

		content := "module testproject\n\ngo 1.25\n\nreplace github.com/test/lib => ../external_lib\nreplace github.com/test/lib2 => ../external_lib2\n"
		os.WriteFile(gomodPath, []byte(content), 0644)

		err := handler.NewFileEvent("go.mod", ".mod", gomodPath, "write")
		if err != nil {
			t.Fatalf("NewFileEvent failed: %v", err)
		}

		absExternal2, _ := filepath.Abs(externalLib2Dir)
		found := false
		for _, v := range watcher.watchedPaths {
			if v == absExternal2 {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Expected new path %s to be watched", absExternal2)
		}
	})

	t.Run("RemovesDirectoryOnReplaceRemoval", func(t *testing.T) {
		// Start with existing watched path (from previous test which added external_lib2,
		// but since we are re-using handler state from previous test, let's reset or just ensure we know what is watched)

		// To be safe and independent:
		// Let's create a fresh scenario for this test or rely on previous state.
		// Previous state has: external_lib (from init) + external_lib2 (from runtime update).

		// Verify initial state for this test
		absExternal2, _ := filepath.Abs(filepath.Join(tmpDir, "external_lib2"))
		isWatched := false
		for _, v := range watcher.watchedPaths {
			if v == absExternal2 {
				isWatched = true
				break
			}
		}
		if !isWatched {
			// This might fail if test order is not guaranteed or previous test failed.
			// But t.Run executes sequentially.
			// Let's re-add it manually to be sure if we want purely independent unit,
			// but here we are testing the state transition of the handler.
			t.Log("Warning: dependent on previous test state")
		}

		// Remove external_lib2 from go.mod
		content := "module testproject\n\ngo 1.25\n\nreplace github.com/test/lib => ../external_lib\n"
		os.WriteFile(gomodPath, []byte(content), 0644)

		err := handler.NewFileEvent("go.mod", ".mod", gomodPath, "write")
		if err != nil {
			t.Fatalf("NewFileEvent failed: %v", err)
		}

		// Verify external_lib2 is removed from watcher
		found := false
		for _, v := range watcher.watchedPaths {
			if v == absExternal2 {
				found = true
				break
			}
		}

		if found {
			t.Errorf("Expected path %s to be REMOVED from watcher, but it is still there", absExternal2)
		}

		// Verify external_lib is still there
		absExternal, _ := filepath.Abs(externalLibDir)
		found = false
		for _, v := range watcher.watchedPaths {
			if v == absExternal {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected path %s to STILL be watched, but it was removed", absExternal)
		}
	})
}

func TestGoModHandler_ObjectsToPublish(t *testing.T) {
	tmp := t.TempDir()
	gomodPath := filepath.Join(tmp, "go.mod")
	content := "module github.com/test/app\n\ngo 1.20\n\nreplace github.com/foo/bar => ../bar\n"
	os.WriteFile(gomodPath, []byte(content), 0644)

	m := devflow.NewGoModHandler()
	ctx := gitmod.PublishContext{RepoDir: tmp, ModulePaths: []string{"github.com/foo/bar"}}

	// with only the expected replace -> ActionNone
	action, reason := m.ObjectsToPublish(ctx)
	if action != gitmod.ActionNone {
		t.Errorf("expected ActionNone, got %v (%s)", action, reason)
	}

	// with another replace unrelated to the module being published -> ActionDepsOnly
	// (the bump must still land in go.mod; only the tag/propagation is withheld)
	content2 := content + "replace github.com/other/lib => ../other\n"
	os.WriteFile(gomodPath, []byte(content2), 0644)
	m = devflow.NewGoModHandler() // fresh load
	action, reason = m.ObjectsToPublish(ctx)
	if action != gitmod.ActionDepsOnly {
		t.Errorf("expected ActionDepsOnly, got %v (%s)", action, reason)
	}
	if reason != gitmod.ObjectionOtherReplaces {
		t.Errorf("expected %q, got %q", gitmod.ObjectionOtherReplaces, reason)
	}
}
