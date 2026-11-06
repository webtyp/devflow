package devflow_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain fails the whole suite when a test touched the real repository
// instead of its temp dir. On 2026-01-19 a test ran `git config user.name Test`
// and `git commit -m initial` here: the identity stayed in .git/config and
// signed 465 commits of devflow as "Test" before anyone saw it.
func TestMain(m *testing.M) {
	before, err := snapshotRepo()
	if err != nil {
		fmt.Fprintln(os.Stderr, "repo guard:", err)
		os.Exit(1)
	}
	code := m.Run()
	after, err := snapshotRepo()
	if err != nil {
		fmt.Fprintln(os.Stderr, "repo guard:", err)
		os.Exit(1)
	}
	if before.head != after.head {
		fmt.Fprintf(os.Stderr, "repo guard: a test committed in the real repository (HEAD %s → %s)\n", before.head, after.head)
		code = 1
	}
	if before.config != after.config {
		fmt.Fprintf(os.Stderr, "repo guard: a test wrote %s\n--- before\n%s--- after\n%s", before.configPath, before.config, after.config)
		code = 1
	}
	os.Exit(code)
}

type repoSnapshot struct {
	head       string
	configPath string
	config     string
}

func snapshotRepo() (repoSnapshot, error) {
	gitDir, err := gitOutput("rev-parse", "--absolute-git-dir")
	if err != nil {
		return repoSnapshot{}, err
	}
	head, err := gitOutput("rev-parse", "HEAD")
	if err != nil {
		return repoSnapshot{}, err
	}
	path := filepath.Join(gitDir, "config")
	config, err := os.ReadFile(path)
	if err != nil {
		return repoSnapshot{}, err
	}
	return repoSnapshot{head: head, configPath: path, config: string(config)}, nil
}

func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
