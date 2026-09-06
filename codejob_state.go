package devflow

import (
	"encoding/json"
	"fmt"
	gitmod "webtyp.com/git"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	keyring "webtyp.com/keyring/auto"
)

// JulesSessionState polls the Jules API for session status.
// Returns (message, prURL, isDone, error).
func JulesSessionState(sessionID, apiKey string, client HTTPClient) (msg, prURL string, done bool, err error) {
	url := fmt.Sprintf("https://jules.googleapis.com/v1alpha/sessions/%s", sessionID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", false, fmt.Errorf("could not create request: %w", err)
	}
	req.Header.Set("X-Goog-Api-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("Jules API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", false, fmt.Errorf("Jules API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var session struct {
		ID      string `json:"id"`
		Outputs []struct {
			PullRequest struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"pullRequest"`
		} `json:"outputs"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", "", false, fmt.Errorf("could not decode Jules response: %w", err)
	}

	for _, out := range session.Outputs {
		if out.PullRequest.URL != "" {
			msg := fmt.Sprintf("✅ Jules: PR ready\n   %s\n   %s", out.PullRequest.Title, out.PullRequest.URL)
			return msg, out.PullRequest.URL, true, nil
		}
	}

	return "⏳ Jules: working...", "", false, nil
}

// CheckoutPRBranch fetches and hard-positions the working tree on the PR's
// head branch. A dirty working tree is handled, not feared: local drift is
// stashed with a labeled stash (CodejobStashMessage) and re-applied after the
// switch; if re-applying conflicts, the stash is KEPT, the conflict files are
// listed, and an error is returned. Returns the branch name on success.
func CheckoutPRBranch(runner gitmod.Runner, prURL string) (string, error) {
	if prURL == "" {
		return "", fmt.Errorf("empty PR URL")
	}

	// 1. git fetch --all
	if _, err := runner.Run("git", "fetch", "--all"); err != nil {
		return "", fmt.Errorf("git fetch --all failed: %w", err)
	}

	// 2. Resolve branch via gh pr view
	branchOut, err := runner.Run("gh", "pr", "view", prURL, "--json", "headRefName", "--jq", ".headRefName")
	if err != nil {
		return "", fmt.Errorf("%s %s", HintManualPRCheckout, prURL)
	}
	branch := strings.TrimSpace(branchOut)
	if branch == "" {
		return "", fmt.Errorf("could not resolve branch name from PR %s", prURL)
	}

	// 3. Check for dirty working tree
	statusOut, _ := runner.Run("git", "status", "--porcelain")
	isDirty := strings.TrimSpace(statusOut) != ""

	// 4. Stash if dirty
	if isDirty {
		if _, err := runner.Run("git", "stash", "push", "-u", "-m", CodejobStashMessage); err != nil {
			return "", fmt.Errorf("git stash failed: %w", err)
		}
	}

	// 5. Checkout
	if _, err := runner.Run("git", "checkout", branch); err != nil {
		// If checkout fails, try to restore stash before returning
		if isDirty {
			_, _ = runner.Run("git", "stash", "pop")
		}
		return "", fmt.Errorf("%s\n    git checkout %s", HintManualCheckout, branch)
	}

	// 6. Verify checkout
	currentBranch, err := runner.Run("git", "branch", "--show-current")
	if err != nil || strings.TrimSpace(currentBranch) != branch {
		return "", fmt.Errorf("checkout verification failed: expected %s, got %s", branch, strings.TrimSpace(currentBranch))
	}

	// 7. Pop stash if we stashed
	if isDirty {
		if out, err := runner.Run("git", "stash", "pop"); err != nil {
			return branch, fmt.Errorf("conflict while re-applying local drift.\nStash kept: %s\nConflicts:\n%s", CodejobStashMessage, out)
		}
	}

	return branch, nil
}

// HandleDone executes cleanup when Jules completes:
// HandleDone is kept for signature compatibility but is a no-op under the new PLAN.md state model.
func HandleDone(runner gitmod.Runner, env *DotEnv, git *gitmod.Git, prURL string) error {
	return nil
}

// MergePR merges the Jules PR and deletes PLAN.md.
func MergePR(runner gitmod.Runner) error {
	meta, err := ReadPlanMeta(DefaultIssuePromptPath)
	if err != nil {
		return fmt.Errorf("could not read PLAN.md: %w", err)
	}
	prURL := meta.PR
	if prURL == "" {
		return fmt.Errorf("no pending PR found in PLAN.md frontmatter")
	}

	// 1. merge PR and delete Jules branch
	var out string
	for i := 0; i < 5; i++ {
		out, err = runner.Run("gh", "pr", "merge", prURL, "--merge", "--delete-branch")
		if err == nil {
			break
		}
		if strings.Contains(out, "is not mergeable") || strings.Contains(err.Error(), "is not mergeable") {
			time.Sleep(2 * time.Second)
			continue
		}
		break
	}
	if err != nil {
		return fmt.Errorf("gh pr merge failed: %w\n%s", err, out)
	}

	// 2. delete docs/PLAN.md
	if _, err := os.Stat(DefaultIssuePromptPath); err == nil {
		if err := os.Remove(DefaultIssuePromptPath); err != nil {
			return fmt.Errorf("could not delete %s: %w", DefaultIssuePromptPath, err)
		}
	}

	return nil
}

// resolveDefaultBranch returns the repo's actual default branch (e.g. "main"
// or "master") by reading the cached origin/HEAD ref, falling back to "main"
// if that ref isn't set locally or the command fails.
func resolveDefaultBranch(runner gitmod.Runner) string {
	out, err := runner.Run("git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	branch := strings.TrimSpace(out)
	if err == nil && branch != "" {
		return strings.TrimPrefix(branch, "origin/")
	}
	return "main"
}

// MergeAndPublish merges the Jules PR, pulls the merged commit, and publishes via gopush.
func MergeAndPublish(runner gitmod.Runner, publisher Publisher, message, overrideTag string) (gitmod.PushResult, error) {
	if err := gitmod.EnsureGHSession(runner, keyring.OpenKeyring("devflow")); err != nil {
		return gitmod.PushResult{}, err
	}
	meta, err := ReadPlanMeta(DefaultIssuePromptPath)
	if err != nil {
		return gitmod.PushResult{}, fmt.Errorf("could not read PLAN.md: %w", err)
	}
	prURL := meta.PR
	if prURL == "" {
		return gitmod.PushResult{}, fmt.Errorf("no pending PR found in PLAN.md frontmatter")
	}

	// 0. Ensure we are on the Jules branch before committing anything
	if _, err := CheckoutPRBranch(runner, prURL); err != nil {
		return gitmod.PushResult{}, err
	}

	// 1. Pre-merge: if working tree is dirty, commit corrections to Jules branch and push
	statusOut, _ := runner.Run("git", "status", "--porcelain")
	if strings.TrimSpace(statusOut) != "" {
		if out, err := runner.Run("git", "add", "."); err != nil {
			return gitmod.PushResult{}, fmt.Errorf("pre-merge git add failed: %w\n%s", err, out)
		}
		if out, err := runner.Run("git", "commit", "-m", "review: corrections before merge"); err != nil {
			return gitmod.PushResult{}, fmt.Errorf("pre-merge commit failed: %w\n%s", err, out)
		}
		if out, err := runner.Run("git", "push"); err != nil {
			return gitmod.PushResult{}, fmt.Errorf("pre-merge push failed: %w\n%s", err, out)
		}
	}

	// Switch to default branch before merging
	defaultBranch := resolveDefaultBranch(runner)
	if out, err := runner.Run("git", "checkout", defaultBranch); err != nil {
		return gitmod.PushResult{}, fmt.Errorf("git checkout %s failed: %w\n%s", defaultBranch, err, out)
	}

	// 2. merge PR and delete Jules branch on GitHub
	var mergeOut string
	var mergeErr error
	for i := 0; i < 5; i++ {
		mergeOut, mergeErr = runner.Run("gh", "pr", "merge", prURL, "--merge", "--delete-branch")
		if mergeErr == nil {
			break
		}
		errMsg := mergeErr.Error()
		if strings.Contains(mergeOut, "is not mergeable") || strings.Contains(errMsg, "is not mergeable") ||
			strings.Contains(mergeOut, "Base branch was modified") || strings.Contains(errMsg, "Base branch was modified") {
			time.Sleep(3 * time.Second)
			continue
		}
		break
	}
	if mergeErr != nil {
		return gitmod.PushResult{}, fmt.Errorf("gh pr merge failed: %w\n%s", mergeErr, mergeOut)
	}

	// 2. pull the merged commit locally
	if _, err := runner.Run("git", "pull"); err != nil {
		return gitmod.PushResult{}, fmt.Errorf("git pull failed: %w", err)
	}

	// 3. remove docs/PLAN.md
	if _, err := os.Stat(DefaultIssuePromptPath); err == nil {
		if err := os.Remove(DefaultIssuePromptPath); err != nil {
			return gitmod.PushResult{}, fmt.Errorf("could not delete %s: %w", DefaultIssuePromptPath, err)
		}
	}

	// No PLAN.md -> call full gopush
	effMsg, effTag, err := ResolvePublishMessage(message, overrideTag, meta)
	if err != nil {
		return gitmod.PushResult{}, err
	}
	return publisher.Publish(effMsg, effTag, false, false, false, false, false, false)
}
