package devflow

import (
	"fmt"
	"webtyp.com/command"
	gitmod "webtyp.com/git"
	"os"
	"path/filepath"
	"strings"
)

// GoNew orchestrator
type GoNew struct {
	git    gitmod.GitClient
	github *Future
	goH    *Go
	log    func(...any)
}

// NewProjectOptions options for creating a new project
type NewProjectOptions struct {
	Name        string // Required, must be valid (alphanumeric, dash, underscore only)
	Description string // Required, max 350 chars
	Owner       string // GitHub owner/organization (default: detected from gh or git config)
	Visibility  string // "public" or "private" (default: "public")
	Directory   string // Supports ~/path, ./path, /abs/path (default: ./{Name})
	LocalOnly   bool   // If true, skip remote creation
	License     string // Default "MIT"
}

// NewGoNew creates orchestrator (all handlers must be initialized)
func NewGoNew(git gitmod.GitClient, github *Future, goHandler *Go) *GoNew {
	return &GoNew{
		git:    git,
		github: github,
		goH:    goHandler,
		log:    func(...any) {},
	}
}

// SetLog sets the logger function
func (gn *GoNew) SetLog(fn func(...any)) {
	if fn != nil {
		gn.log = fn
		if gn.git != nil {
			gn.git.SetLog(fn)
		}
		// Note: GitHub client uses its own logger set during initialization
		// We don't update it here to avoid race conditions with the Future
		if gn.goH != nil {
			gn.goH.SetLog(fn)
		}
	}
}

// Create executes full workflow with remote (or local-only fallback)
func (gn *GoNew) Create(opts NewProjectOptions) (string, error) {
	// 1. Validate inputs
	if err := ValidateRepoName(opts.Name); err != nil {
		return "", err
	}
	if err := ValidateDescription(opts.Description); err != nil {
		return "", err
	}

	if opts.Visibility == "" {
		opts.Visibility = "public"
	}

	// Determine target directory
	targetDir := opts.Directory
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		targetDir = filepath.Join(cwd, opts.Name)
	}
	// Expand home tilde if present (simple handle)
	if strings.HasPrefix(targetDir, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			targetDir = filepath.Join(home, targetDir[2:])
		}
	}
	targetDir, _ = filepath.Abs(targetDir)

	// 2. Check availability
	// Check if directory exists
	if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
		return "", fmt.Errorf("directory %s already exists", targetDir)
	}

	// Prepare result summary
	var resultSummary string
	isRemote := false

	// Check git config
	userName, err := gn.git.GetConfigUserName()
	if err != nil || userName == "" {
		return "", fmt.Errorf("git user.name not configured. Run: git config --global user.name \"Name\"")
	}
	_, err = gn.git.GetConfigUserEmail()
	if err != nil {
		// Email is not strictly required for license but needed for commit usually
		return "", fmt.Errorf("git user.email not configured. Run: git config --global user.email \"email@example.com\"")
	}

	// 3. Determine owner
	var ghUser string
	if opts.Owner != "" {
		// Use specified owner
		ghUser = opts.Owner
	} else if gn.github != nil {
		// Auto-detect from gh CLI
		res, err := gn.github.Get()
		if err != nil {
			return "", err
		}
		gh := res.(gitmod.GitHubClient)

		ghUser, err = gh.GetCurrentUser()
		if err != nil && !opts.LocalOnly {
			// Fallback to git config if gh fails
			gitUser := strings.ReplaceAll(strings.ToLower(userName), " ", "")
			ghUser = gitUser
			gn.log("Warning: could not get GitHub user, using git user:", gitUser)
		}
	} else {
		// Fallback to git config
		ghUser = strings.ReplaceAll(strings.ToLower(userName), " ", "")
	}

	// 4. Create remote (if not local-only)
	// We'll create the empty repo first, then add remote after local setup
	if !opts.LocalOnly {
		// Check if repo exists on GitHub
		res, err := gn.github.Get()
		if err != nil {
			return "", err
		}
		gh := res.(gitmod.GitHubClient)

		if ghUser == "" {
			ghUser, err = gh.GetCurrentUser()
		}
		if err != nil {
			// Fallback to local only
			gn.log("GitHub unavailable:", err)
			resultSummary = fmt.Sprintf("⚠️ Created: %s [local only] v0.0.1 - %s", opts.Name, gh.GetHelpfulErrorMessage(err))
		} else {
			exists, err := gh.RepoExists(ghUser, opts.Name)
			if err == nil && exists {
				return "", fmt.Errorf("repository %s/%s already exists on GitHub", ghUser, opts.Name)
			} else if err != nil {
				// Network error or other issue
				gn.log("GitHub check failed:", err)
				resultSummary = fmt.Sprintf("⚠️ Created: %s [local only] v0.0.1 - gh unavailable", opts.Name)
			} else {
				// Create empty remote repo
				if err := gh.CreateRepo(ghUser, opts.Name, opts.Description, opts.Visibility); err != nil {
					gn.log("Failed to create remote:", err)
					resultSummary = fmt.Sprintf("⚠️ Created: %s [local only] v0.0.1 - failed to create remote", opts.Name)
				} else {
					isRemote = true
					resultSummary = fmt.Sprintf("✅ Created: %s [local+remote] v0.0.1", opts.Name)
				}
			}
		}
	} else {
		resultSummary = fmt.Sprintf("⚠️ Created: %s [local only] v0.0.1 - run 'gonew add-remote' when ready", opts.Name)
	}

	// 5. Initialize local directory
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Always init local (don't clone, we'll add remote later)
	if err := gn.git.InitRepo(targetDir); err != nil {
		return "", fmt.Errorf("failed to init repo: %w", err)
	}

	// CRITICAL: Update git handler root to the new project directory
	// so future commands (add, commit, push) run there.
	gn.git.SetRootDir(targetDir)

	// 6. Generate files
	if err := GenerateREADME(opts.Name, opts.Description, targetDir); err != nil {
		return "", err
	}
	if err := GenerateLicense(userName, targetDir); err != nil {
		return "", err
	}
	if err := GenerateGitignore(targetDir); err != nil {
		return "", err
	}
	if err := GenerateHandlerFile(opts.Name, targetDir); err != nil {
		return "", err
	}

	// Go Mod Init
	modulePath := fmt.Sprintf("github.com/%s/%s", ghUser, opts.Name)

	if err := gn.goH.ModInit(modulePath, targetDir); err != nil {
		return "", fmt.Errorf("go mod init failed: %w", err)
	}

	// Change to target dir for git operations
	originalDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(targetDir); err != nil {
		return "", err
	}

	// 7. Initial commit
	if err := gn.git.Add(); err != nil {
		return "", err
	}
	if _, err := gn.git.Commit("Initial commit"); err != nil {
		return "", err
	}

	// 8. Tag creation
	if _, err := gn.git.CreateTag("v0.0.1"); err != nil {
		return "", err
	}

	// 9. Add remote and push (if remote was created)
	if isRemote {
		// Add remote origin
		repoURL := fmt.Sprintf("https://github.com/%s/%s.git", ghUser, opts.Name)
		if _, err := command.Run("git", "remote", "add", "origin", repoURL); err != nil {
			gn.log("Failed to add remote:", err)
			resultSummary = fmt.Sprintf("⚠️ Created: %s [local only] v0.0.1 - failed to add remote", opts.Name)
		} else if _, err := gn.git.PushWithTags("v0.0.1"); err != nil {
			// If push fails, warn but don't fail the whole process
			gn.log("Push failed:", err)
			resultSummary = fmt.Sprintf("⚠️ Created: %s [local only] v0.0.1 - push failed", opts.Name)
		}
	}

	return resultSummary, nil
}

// AddRemote adds GitHub remote to existing local project
func (gn *GoNew) AddRemote(projectPath, visibility, owner string) (string, error) {
	// ... Implement AddRemote logic ...
	// For now, let's implement the basic structure based on spec.

	targetDir := projectPath
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		targetDir = cwd
	}
	// Expand path...

	targetDir, _ = filepath.Abs(targetDir)

	// Validate project structure
	if _, err := os.Stat(filepath.Join(targetDir, "go.mod")); os.IsNotExist(err) {
		return "", fmt.Errorf("not a Go project (go.mod missing)")
	}
	if _, err := os.Stat(filepath.Join(targetDir, ".git")); os.IsNotExist(err) {
		return "", fmt.Errorf("not a git repository")
	}

	// Read description from README.md (assuming first line # Name\n\nDesc)
	// Or just use a default? Spec says "reads description from README.md"
	description := "Go project" // Default
	readmeBytes, err := os.ReadFile(filepath.Join(targetDir, "README.md"))
	if err == nil {
		// Try to parse
		lines := strings.Split(string(readmeBytes), "\n")
		// Look for first non-empty line after title?
		for i, line := range lines {
			if strings.HasPrefix(line, "#") {
				// Title
				if i+2 < len(lines) {
					desc := strings.TrimSpace(lines[i+2])
					if desc != "" {
						description = desc
					}
				}
				break
			}
		}
	}

	// Repo name from dir name
	repoName := filepath.Base(targetDir)

	// Check if remote exists locally
	originalDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(targetDir); err != nil {
		return "", err
	}

	// Check existing remotes
	remotes, _ := command.Run("git", "remote")
	if strings.Contains(remotes, "origin") {
		return fmt.Sprintf("Remote 'origin' already configured for %s", repoName), nil
	}

	// Determine owner
	var ghUser string
	if owner != "" {
		ghUser = owner
	} else {
		res, err := gn.github.Get()
		if err != nil {
			return "", err
		}
		gh := res.(gitmod.GitHubClient)

		ghUser, err = gh.GetCurrentUser()
		if err != nil {
			return "", fmt.Errorf("GitHub unavailable: %w", err)
		}
	}

	res, err := gn.github.Get()
	if err != nil {
		return "", err
	}
	gh := res.(gitmod.GitHubClient)

	exists, err := gh.RepoExists(ghUser, repoName)
	if err == nil && exists {
		return "", fmt.Errorf("repository %s/%s already exists on GitHub", ghUser, repoName)
	}

	// Create remote
	if visibility == "" {
		visibility = "public"
	}
	if err := gh.CreateRepo(ghUser, repoName, description, visibility); err != nil {
		return "", fmt.Errorf("failed to create remote: %w", err)
	}

	// Add remote
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", ghUser, repoName)
	if _, err := command.Run("git", "remote", "add", "origin", repoURL); err != nil {
		return "", fmt.Errorf("failed to add remote: %w", err)
	}

	// Push
	// We need to push current branch to main
	// And push tags
	if _, err := gn.git.PushWithTags("v0.0.1"); err != nil {
		// If fails, maybe we need to push plain first?
		// Or maybe v0.0.1 doesn't exist?
		// Try pushing HEAD
		if _, err := command.Run("git", "push", "-u", "origin", "main"); err != nil {
			return "", fmt.Errorf("failed to push: %w", err)
		}
		// Try pushing tags if any
		command.Run("git", "push", "--tags")
	}

	return fmt.Sprintf("✅ Remote added: %s/%s", ghUser, repoName), nil
}
