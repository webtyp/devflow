//go:build integration

package devflow_test

import "webtyp.com/devflow"

import (
	gitmod "webtyp.com/git"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectTemplates(t *testing.T) {
	tmpDir := t.TempDir()

	// Test devflow.ValidateRepoName
	if err := devflow.ValidateRepoName("valid-name_123"); err != nil {
		t.Errorf("devflow.ValidateRepoName failed for valid name: %v", err)
	}
	if err := devflow.ValidateRepoName("invalid name"); err == nil {
		t.Error("devflow.ValidateRepoName should fail for invalid name")
	}

	// Test devflow.ValidateDescription
	if err := devflow.ValidateDescription("valid desc"); err != nil {
		t.Errorf("devflow.ValidateDescription failed for valid desc: %v", err)
	}
	if err := devflow.ValidateDescription(""); err == nil {
		t.Error("devflow.ValidateDescription should fail for empty desc")
	}

	// Test devflow.GenerateREADME
	if err := devflow.GenerateREADME("my-repo", "desc", tmpDir); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(tmpDir, "README.md"))
	if string(content) != "# my-repo\n\ndesc\n" {
		t.Errorf("devflow.GenerateREADME content mismatch: %s", string(content))
	}

	// Test devflow.GenerateLicense
	if err := devflow.GenerateLicense("Owner", tmpDir); err != nil {
		t.Fatal(err)
	}
	// Check content contains owner
	content, _ = os.ReadFile(filepath.Join(tmpDir, "LICENSE"))
	if !strings.Contains(string(content), "Owner") {
		t.Errorf("devflow.GenerateLicense content should contain 'Owner', got:\n%s", string(content))
	}

	// Test devflow.GenerateHandlerFile
	if err := devflow.GenerateHandlerFile("my-repo", tmpDir); err != nil {
		t.Fatal(err)
	}
	content, _ = os.ReadFile(filepath.Join(tmpDir, "my-repo.go"))
	expected := `package myrepo

type MyRepo struct {}

func New() *MyRepo {
    return &MyRepo{}
}
`
	if string(content) != expected {
		t.Errorf("devflow.GenerateHandlerFile content mismatch. Got:\n%s\nExpected:\n%s", string(content), expected)
	}
}

func TestKebabToCamel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-repo", "MyRepo"},
		{"my_repo", "MyRepo"},
		{"repo", "Repo"},
		{"my-long-repo-name", "MyLongRepoName"},
	}

	for _, test := range tests {
		if got := devflow.KebabToCamel(test.input); got != test.expected {
			t.Errorf("devflow.KebabToCamel(%q) = %q, want %q", test.input, got, test.expected)
		}
	}
}

func TestGoNewCreateLocalOnly(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()

	// We need to change cwd because Create uses relative paths or defaults
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	// Mock Git config
	// We can set it globally or locally?
	// NewGit checks if git is installed.
	git, err := gitmod.NewGit()
	if err != nil {
		t.Skip("git not installed")
	}

	// Configure git user for this test
	// We might need to run git config globally? Or just mock the GetConfigUserName method?
	// We can't mock methods of a struct easily in Go without interfaces.
	// So we rely on git config being present or set it.
	// We use HOME env var to isolate git config for this test.
	os.Setenv("HOME", tmpDir)
	// Create a .gitconfig in tmpDir
	gitConfig := `[user]
	name = TestUser
	email = test@example.com
`
	os.WriteFile(filepath.Join(tmpDir, ".gitconfig"), []byte(gitConfig), 0644)
	// We also need to make sure git picks it up. Git looks at HOME.

	goHandler, _ := devflow.NewGo(git)

	// Mock GitHub (nil or real?)
	// For local-only, we can pass nil if we handle it?
	// But devflow.NewGoNew expects *GitHub.
	// We can create one but we can't easily mock devflow.RunCommand.
	// So let's create a real one (it just calls gh).
	// If gh is not installed, NewGitHub returns error.
	// If we want to test local-only, we should be able to do it even without gh.
	// In the CLI we pass nil if gh fails.
	// So let's pass nil here and see if Create handles it (we implemented checks).

	gn := devflow.NewGoNew(git, nil, goHandler)

	opts := devflow.NewProjectOptions{
		Name:        "test-project",
		Description: "A test project",
		LocalOnly:   true,
		Directory:   filepath.Join(tmpDir, "test-project"),
	}

	summary, err := gn.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if summary == "" {
		t.Error("Summary should not be empty")
	}

	// Verify files
	targetDir := opts.Directory
	files := []string{"README.md", "LICENSE", ".gitignore", "go.mod", "test-project.go"}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(targetDir, f)); os.IsNotExist(err) {
			t.Errorf("File %s not created", f)
		}
	}

	// Verify git init and commit
	if _, err := os.Stat(filepath.Join(targetDir, ".git")); os.IsNotExist(err) {
		t.Error(".git directory not created")
	}
}

func TestGoNewWithCustomOwner(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	git, err := gitmod.NewGit()
	if err != nil {
		t.Skip("git not installed")
	}

	// Configure git user
	os.Setenv("HOME", tmpDir)
	gitConfig := `[user]
	name = TestUser
	email = test@example.com
`
	os.WriteFile(filepath.Join(tmpDir, ".gitconfig"), []byte(gitConfig), 0644)

	goHandler, _ := devflow.NewGo(git)
	gn := devflow.NewGoNew(git, nil, goHandler)

	// Test with custom owner
	opts := devflow.NewProjectOptions{
		Name:        "test-project",
		Description: "A test project",
		Owner:       "cdvelop",
		LocalOnly:   true,
		Directory:   filepath.Join(tmpDir, "test-project"),
	}

	summary, err := gn.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !strings.Contains(summary, "test-project") {
		t.Error("Summary should contain project name")
	}

	// Verify go.mod contains correct module path with custom owner
	targetDir := opts.Directory
	goModContent, err := os.ReadFile(filepath.Join(targetDir, "go.mod"))
	if err != nil {
		t.Fatalf("Failed to read go.mod: %v", err)
	}

	expectedModulePath := "module github.com/cdvelop/test-project"
	if !strings.Contains(string(goModContent), expectedModulePath) {
		t.Errorf("go.mod should contain '%s', got:\n%s", expectedModulePath, string(goModContent))
	}
}
