package devflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

func TestGoVersionRootDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "devflow-test-version-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	goModContent := "module testversion\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	g, err := devflow.NewGo(nil)
	if err != nil {
		t.Fatal(err)
	}
	g.SetRootDir(tmpDir)

	version, err := g.GoVersion()
	if err != nil {
		t.Fatalf("GoVersion failed: %v", err)
	}

	if version != "1.21" {
		t.Errorf("expected version 1.21, got %s", version)
	}
}

func TestVerifyRootDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "devflow-test-verify-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a minimal valid module
	goModContent := "module testverify\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	g, err := devflow.NewGo(nil)
	if err != nil {
		t.Fatal(err)
	}
	g.SetRootDir(tmpDir)

	// Verify should pass in a clean module
	if err := g.Verify(); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
}

func TestBadgesRootDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "devflow-test-badges-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a dummy README.md
	readmeContent := "# Test Project\n\nExisting content."
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte(readmeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a dummy go.mod for getModuleName used in badges
	goModContent := "module testbadges\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	bh := devflow.NewBadges("License:MIT:blue", "Go:1.21:blue")
	bh.SetRootDir(tmpDir)

	if _, err := bh.BuildBadges(); err != nil {
		t.Fatalf("BuildBadges failed: %v", err)
	}

	// Check if badges.svg was created in rootDir
	svgPath := filepath.Join(tmpDir, "docs/img/badges.svg")
	if _, err := os.Stat(svgPath); os.IsNotExist(err) {
		t.Errorf("badges.svg not found at %s", svgPath)
	}

	// Check if README.md was updated in rootDir
	updatedReadme, err := os.ReadFile(filepath.Join(tmpDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(updatedReadme), "badges.svg") {
		t.Errorf("README.md does not contain badge link")
	}
}
