package devflow_test

import (
	"os"
	"path/filepath"
	"testing"

	"webtyp.com/devflow"
)

func TestWorkspaceRootFrom(t *testing.T) {
	home := t.TempDir()

	// Structure:
	// <home>/Dev/Project/Project.code-workspace
	// <home>/Dev/Project/org_a/lib/
	// <home>/Dev/Project/org_b/velty.code-workspace
	// <home>/Dev/Project/org_b/app/
	projDir := filepath.Join(home, "Dev", "Project")
	orgADir := filepath.Join(projDir, "org_a", "lib")
	orgBVeltyDir := filepath.Join(projDir, "org_b")
	orgBAppDir := filepath.Join(orgBVeltyDir, "app")

	if err := os.MkdirAll(orgADir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(orgBAppDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create Project.code-workspace in projDir
	if err := os.WriteFile(filepath.Join(projDir, "Project.code-workspace"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create velty.code-workspace in orgBVeltyDir
	if err := os.WriteFile(filepath.Join(orgBVeltyDir, "velty.code-workspace"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Case 1: WorkspaceRootFrom(".../org_a/lib", home) -> <home>/Dev/Project
	got1 := devflow.WorkspaceRootFrom(orgADir, home)
	if got1 != projDir {
		t.Errorf("Case 1 expected %q, got %q", projDir, got1)
	}

	// Case 2: WorkspaceRootFrom(".../org_b/app", home) -> <home>/Dev/Project (outermost, not org_b)
	got2 := devflow.WorkspaceRootFrom(orgBAppDir, home)
	if got2 != projDir {
		t.Errorf("Case 2 expected %q (outermost), got %q", projDir, got2)
	}

	// Case 3: No *.code-workspace in tree -> ""
	noWorkspaceHome := t.TempDir()
	someDir := filepath.Join(noWorkspaceHome, "a", "b")
	if err := os.MkdirAll(someDir, 0755); err != nil {
		t.Fatal(err)
	}
	got3 := devflow.WorkspaceRootFrom(someDir, noWorkspaceHome)
	if got3 != "" {
		t.Errorf("Case 3 expected empty string, got %q", got3)
	}

	// Case 4: A *.code-workspace directly in home is ignored -> ""
	homeWithWs := t.TempDir()
	subDir := filepath.Join(homeWithWs, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeWithWs, "user.code-workspace"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	got4 := devflow.WorkspaceRootFrom(subDir, homeWithWs)
	if got4 != "" {
		t.Errorf("Case 4 expected empty string when workspace marker is directly in home, got %q", got4)
	}

	// Case 5: A directory named foo.code-workspace is ignored
	dirWsHome := t.TempDir()
	subDir5 := filepath.Join(dirWsHome, "Project", "sub")
	dirWs := filepath.Join(dirWsHome, "Project", "dir.code-workspace")
	if err := os.MkdirAll(subDir5, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirWs, 0755); err != nil {
		t.Fatal(err)
	}
	got5 := devflow.WorkspaceRootFrom(subDir5, dirWsHome)
	if got5 != "" {
		t.Errorf("Case 5 expected empty string for directory workspace marker, got %q", got5)
	}
}

func TestFindDependentModulesAndGraph_SkipsNodeModulesAndHidden(t *testing.T) {
	home := t.TempDir()

	// Structure:
	// <home>/Dev/Project/Project.code-workspace
	// <home>/Dev/Project/org_a/lib/go.mod (module example.com/lib)
	// <home>/Dev/Project/org_b/app/go.mod (require example.com/lib v0.0.1)
	// <home>/Dev/Project/org_b/app/node_modules/x/go.mod (require example.com/lib v0.0.1)
	projDir := filepath.Join(home, "Dev", "Project")
	orgALibDir := filepath.Join(projDir, "org_a", "lib")
	orgBAppDir := filepath.Join(projDir, "org_b", "app")
	nodeModDir := filepath.Join(orgBAppDir, "node_modules", "x")

	for _, d := range []string{orgALibDir, orgBAppDir, nodeModDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(projDir, "Project.code-workspace"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write go.mod for org_a/lib
	libMod := "module example.com/lib\n\ngo 1.20\n"
	if err := os.WriteFile(filepath.Join(orgALibDir, "go.mod"), []byte(libMod), 0644); err != nil {
		t.Fatal(err)
	}

	// Write go.mod for org_b/app
	appMod := "module example.com/app\n\ngo 1.20\n\nrequire example.com/lib v0.0.1\n"
	if err := os.WriteFile(filepath.Join(orgBAppDir, "go.mod"), []byte(appMod), 0644); err != nil {
		t.Fatal(err)
	}

	// Write go.mod in node_modules/x
	nodeMod := "module example.com/x\n\ngo 1.20\n\nrequire example.com/lib v0.0.1\n"
	if err := os.WriteFile(filepath.Join(nodeModDir, "go.mod"), []byte(nodeMod), 0644); err != nil {
		t.Fatal(err)
	}

	g := newGoHandlerWithMockBackup(t, &MockGitClient{})
	g.SetRootDir(orgALibDir)

	// Case 6: FindDependentModules("example.com/lib", projDir)
	deps, err := g.FindDependentModules("example.com/lib", projDir)
	if err != nil {
		t.Fatalf("FindDependentModules: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected exactly 1 dependent (org_b/app), got %d: %v", len(deps), deps)
	}
	if deps[0] != orgBAppDir {
		t.Errorf("expected dependent %q, got %q", orgBAppDir, deps[0])
	}

	// Case 7: BuildDependentGraph("example.com/lib", projDir)
	nodes, err := g.BuildDependentGraph("example.com/lib", projDir)
	if err != nil {
		t.Fatalf("BuildDependentGraph: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node in dependent graph, got %d: %+v", len(nodes), nodes)
	}
	if nodes[0].ModulePath != "example.com/app" {
		t.Errorf("expected node module example.com/app, got %q", nodes[0].ModulePath)
	}
}
