package devflow

import (
	"fmt"
	gitmod "webtyp.com/git"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"webtyp.com/context"
	keyring "webtyp.com/keyring/auto"
	"webtyp.com/wizard"
	"golang.org/x/term"
)

const (
	// DefaultIssuePromptPath is the conventional location for the task description file.
	DefaultIssuePromptPath = "docs/PLAN.md"

	// CodejobStashMessage is the label for the git stash created during branch switching.
	CodejobStashMessage = "codejob: local drift before review"

	// HintManualCheckout is the message shown when automatic branch switch fails.
	HintManualCheckout = "⚠️  Could not switch branch automatically — run manually:"
	// HintManualPRCheckout is the message shown when PR branch resolution fails.
	HintManualPRCheckout = "⚠️  Could not resolve branch from PR — switch manually:"
)

type CodejobPhase string

const (
	PhaseRunning CodejobPhase = "running"
	PhaseReview  CodejobPhase = "review"
)

// CodeJob orchestrates sending a coding task to a chain of AI agent drivers.
// It validates the prompt file, then tries each driver in priority order,
// falling back to the next on failure.
type CodeJob struct {
	drivers   []CodeJobDriver
	log       func(...any)
	publisher Publisher
	releaseFn func(tag string) error
	runner    gitmod.Runner
}

// NewCodeJob creates a CodeJob with the given ordered drivers.
func NewCodeJob(drivers ...CodeJobDriver) *CodeJob {
	return &CodeJob{
		drivers: drivers,
		log:     func(...any) {},
		runner:  gitmod.RealRunner{},
	}
}

// SetLog sets the logging function for the orchestrator.
func (c *CodeJob) SetLog(fn func(...any)) {
	if fn != nil {
		c.log = fn
	}
}

// SetRunner sets the command runner (mainly for testing).
func (c *CodeJob) SetRunner(r gitmod.Runner) {
	if r != nil {
		c.runner = r
	}
}

// ObjectsToPublish implements PublishObjector. It is stateless.
func (CodeJob) ObjectsToPublish(ctx gitmod.PublishContext) (gitmod.PublishAction, string) {
	// Skip if a session is active (running or review).
	// Running: agent is working, do not touch.
	// Review: local tree is on PR branch, deps updates would commit into the PR.
	phase := CodejobPhaseOf(ctx.RepoDir)
	if phase == PhaseRunning || phase == PhaseReview {
		return gitmod.ActionSkip, gitmod.ObjectionCodejobSession
	}
	if _, err := os.Stat(filepath.Join(ctx.RepoDir, DefaultIssuePromptPath)); err == nil {
		return gitmod.ActionDepsOnly, gitmod.ObjectionPlanPending
	}
	return gitmod.ActionNone, ""
}

// SetPublisher injects a Publisher for close-loop operations.
func (c *CodeJob) SetPublisher(p Publisher) { c.publisher = p }

// SetReleaser injects a release function to be called after MergeAndPublish when -release flag is used.
func (c *CodeJob) SetReleaser(fn func(tag string) error) { c.releaseFn = fn }

// IsEnvironmentValid reports whether the current working directory has an
// active codejob context: a PLAN.md to dispatch or a session in progress.
func IsEnvironmentValid(dotenvPath string) bool {
	if _, err := os.Stat(DefaultIssuePromptPath); err == nil {
		return true
	}
	return false
}

// Run implements the unified API logic.
// isRelease indicates whether to create a GitHub Release after MergeAndPublish.
func (c *CodeJob) Run(message, tag string, isRelease bool) (string, error) {
	// If PLAN.md is missing, we cannot do anything
	if _, err := os.Stat(DefaultIssuePromptPath); os.IsNotExist(err) {
		return "", fmt.Errorf("prompt file not found: %s", DefaultIssuePromptPath)
	}

	meta, err := ReadPlanMeta(DefaultIssuePromptPath)
	if err != nil {
		return "", err
	}

	// Status derivation
	if meta.Status == "" {
		meta.Status = "dispatch"
	}

	// 1. If message provided (or status is review) -> close the loop
	if message != "" || strings.ToLower(meta.Status) == "review" {
		if c.publisher == nil {
			return "", fmt.Errorf("no publisher configured")
		}
		res, err := MergeAndPublish(c.runner, c.publisher, message, tag)
		if err != nil {
			return "", err
		}

		// If -release flag is set and releaseFn is configured, create the release
		if isRelease && c.releaseFn != nil {
			if err := c.releaseFn(res.Tag); err != nil {
				return res.Summary, fmt.Errorf("release creation failed: %w", err)
			}
		}

		return res.Summary, nil
	}

	// 2. STATUS is running -> check status
	if strings.ToLower(meta.Status) == "running" {
		return c.checkStatus(meta)
	}

	// 3. STATUS is reviewing -> wait for review / check reviews
	if strings.ToLower(meta.Status) == "reviewing" {
		return "⏳ Reviewer is reviewing the PR...", nil
	}

	// 4. Auto-setup if API key missing
	auth, err := NewJulesAuth()
	if err == nil && !auth.HasKey() {
		if err := c.runSetupWizard(); err != nil {
			return "", err
		}
	}

	// 5. Dispatch
	return c.Send(DefaultIssuePromptPath)
}

func (c *CodeJob) checkStatus(meta PlanMeta) (string, error) {
	sessionID := meta.Session
	if sessionID == "" {
		return "", fmt.Errorf("no active session found in PLAN.md frontmatter")
	}

	auth, _ := NewJulesAuth()
	apiKey, err := auth.EnsureAPIKey()
	if err != nil {
		return "", err
	}

	msg, prURL, done, err := JulesSessionState(sessionID, apiKey, &http.Client{})
	if err != nil {
		return "", err
	}

	if done {
		// Checkout PR branch
		if _, err := CheckoutPRBranch(c.runner, prURL); err != nil {
			return msg, fmt.Errorf("checkout PR branch failed: %w", err)
		}

		// Update PLAN.md frontmatter status
		meta.PR = prURL
		if meta.Reviewer == "" || strings.ToLower(meta.Reviewer) == "none" {
			meta.Status = "review"
		} else {
			meta.Status = "reviewing"
		}
		if err := WritePlanMeta(DefaultIssuePromptPath, meta); err != nil {
			return msg, fmt.Errorf("could not update PLAN.md: %w", err)
		}

		// Commit transition to git
		_, _ = c.runner.Run("git", "add", DefaultIssuePromptPath)
		commitMsg := fmt.Sprintf("chore: status transition to %s [pr %s]", meta.Status, prURL)
		_, _ = c.runner.Run("git", "commit", "-m", commitMsg)
		_, _ = c.runner.Run("git", "push")
	}

	return msg, nil
}

func (c *CodeJob) runSetupWizard() error {
	wiz := wizard.New(func(_ *context.Context) {
		fmt.Println("\n✅ Jules API key saved. Run 'codejob' to dispatch a task.")
	}, c)

	for wiz.WaitingForUser() {
		label := wiz.Label()
		fmt.Fprintf(os.Stderr, "\n%s: ", label)
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("could not read API key: %w", err)
		}
		wiz.Change(string(raw))
		if !wiz.WaitingForUser() {
			break
		}
	}
	return nil
}

// GetSteps for wizard
func (c *CodeJob) GetSteps() []*wizard.Step {
	return []*wizard.Step{
		{
			LabelText: "Jules API Key (get yours at " + termLink(julesAPIKeyURL, julesAPIKeyURL) + ")",
			DefaultFn: func(ctx *context.Context) string { return "" },
			OnInputFn: func(in string, ctx *context.Context) (bool, error) {
				in = strings.TrimSpace(in)
				if in == "" {
					return false, fmt.Errorf("API key cannot be empty")
				}
				kr, err := keyring.NewKeyring("devflow")
				if err != nil {
					return false, fmt.Errorf("could not initialize keyring: %w", err)
				}
				if err := kr.Set(julesAPIKeyKey, in); err != nil {
					c.log(fmt.Sprintf("warning: could not save API key to keyring: %v", err))
				}
				return true, nil
			},
		},
	}
}

// Send validates issuePromptPath, publishes pending changes, then tries each
// driver in order until one succeeds. Returns an error if the file is missing,
// empty, the publish fails, or all drivers fail.
func (c *CodeJob) Send(issuePromptPath string) (string, error) {
	if err := gitmod.EnsureGHSession(c.runner, keyring.OpenKeyring("devflow")); err != nil {
		return "", err
	}

	meta, err := ReadPlanMeta(issuePromptPath)
	if err != nil {
		return "", fmt.Errorf("invalid plan frontmatter in %s: %w", issuePromptPath, err)
	}

	info, err := os.Stat(issuePromptPath)
	if err != nil {
		return "", fmt.Errorf("prompt file not found: %w", err)
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("prompt file is empty: %s", issuePromptPath)
	}

	// PUBLISH BEFORE SEND (Stage 2, step 2.3)
	if c.publisher != nil {
		_, err := c.publisher.Publish("chore: sync before codejob dispatch", "", true, true, true, true, true, true)
		if err != nil {
			return "", fmt.Errorf("failed to sync repo before dispatch: %w", err)
		}
	}

	if len(c.drivers) == 0 {
		return "", fmt.Errorf("no drivers configured")
	}

	prompt := "Execute the implementation plan described in " + issuePromptPath
	title := autoDetectTitle()

	var lastErr error
	for _, d := range c.drivers {
		d.SetLog(c.log)
		result, err := d.Send(prompt, title)
		if err == nil {
			if sp, ok := d.(SessionProvider); ok {
				if id := sp.SessionID(); id != "" {
					meta.Status = "running"
					meta.Session = id
					_ = WritePlanMeta(issuePromptPath, meta)

					// Commit transition to git
					_, _ = c.runner.Run("git", "add", issuePromptPath)
					commitMsg := fmt.Sprintf("chore: status transition to running [session %s]", id)
					_, _ = c.runner.Run("git", "commit", "-m", commitMsg)
					_, _ = c.runner.Run("git", "push")
				}
			}
			return result, nil
		}
		c.log(fmt.Sprintf("driver %s failed: %v", d.Name(), err))
		lastErr = err
	}

	return "", fmt.Errorf("all agents failed, last error: %w", lastErr)
}

// autoDetectTitle returns "owner/repo" for the current git repository,
// or "" if detection fails (non-fatal; driver will use its own fallback).
func autoDetectTitle() string {
	owner, repo, err := autoDetectOwnerRepo()
	if err != nil {
		return ""
	}
	return owner + "/" + repo
}

// RunCI executes a single state transition phase for the codejob orchestrator in CI.
func (c *CodeJob) RunCI(phase string) error {
	if _, err := os.Stat(DefaultIssuePromptPath); os.IsNotExist(err) {
		if phase == "publish" {
			// "publish no-op si falta el plan"
			return nil
		}
		return fmt.Errorf("docs/PLAN.md not found")
	}

	meta, err := ReadPlanMeta(DefaultIssuePromptPath)
	if err != nil {
		return err
	}

	switch strings.ToLower(phase) {
	case "dispatch":
		if meta.Status != "" && strings.ToLower(meta.Status) != "dispatch" {
			return fmt.Errorf("cannot dispatch: STATUS is %q", meta.Status)
		}
		_, err := c.Send(DefaultIssuePromptPath)
		return err

	case "review":
		if strings.ToLower(meta.Status) != "running" {
			return fmt.Errorf("cannot review: STATUS is %q (expected running)", meta.Status)
		}
		// pull_request opened -> reviewing (REVIEWER set) or review (REVIEWER none)
		if meta.Reviewer == "" || strings.ToLower(meta.Reviewer) == "none" {
			meta.Status = "review"
			if err := WritePlanMeta(DefaultIssuePromptPath, meta); err != nil {
				return err
			}
			_, _ = c.runner.Run("git", "add", DefaultIssuePromptPath)
			_, _ = c.runner.Run("git", "commit", "-m", "chore: no reviewer set, status to review")
			_, _ = c.runner.Run("git", "push")
			return nil
		}

		// REVIEWER set -> reviewing
		meta.Status = "reviewing"
		// Dispatch reviewer
		reviewerSessionID := "R-" + meta.Session
		meta.ReviewSession = reviewerSessionID
		if err := WritePlanMeta(DefaultIssuePromptPath, meta); err != nil {
			return err
		}
		_, _ = c.runner.Run("git", "add", DefaultIssuePromptPath)
		_, _ = c.runner.Run("git", "commit", "-m", "chore: status transition to reviewing")
		_, _ = c.runner.Run("git", "push")
		return nil

	case "verdict":
		if strings.ToLower(meta.Status) != "reviewing" {
			return fmt.Errorf("cannot verdict: STATUS is %q (expected reviewing)", meta.Status)
		}

		// Read review state via gitmod.Runner (e.g. gh pr view <PR> --json reviews --jq ".reviews")
		reviewsJSON, err := c.runner.Run("gh", "pr", "view", meta.PR, "--json", "reviews", "--jq", ".reviews")
		if err != nil {
			return fmt.Errorf("failed to fetch reviews: %w", err)
		}

		// Simple mock-friendly check for review state
		isApproved := strings.Contains(reviewsJSON, "APPROVED")
		isChangesRequested := strings.Contains(reviewsJSON, "CHANGES_REQUESTED")

		if isApproved {
			meta.Status = "review"
			if err := WritePlanMeta(DefaultIssuePromptPath, meta); err != nil {
				return err
			}
			_, _ = c.runner.Run("git", "add", DefaultIssuePromptPath)
			_, _ = c.runner.Run("git", "commit", "-m", "chore: reviewer approved, status to review")
			_, _ = c.runner.Run("git", "push")
			return nil
		}

		if isChangesRequested {
			meta.Round++
			if meta.Round > 3 {
				// Round cap exceeded -> hand over to human (status review)
				meta.Status = "review"
				if err := WritePlanMeta(DefaultIssuePromptPath, meta); err != nil {
					return err
				}
				_, _ = c.runner.Run("git", "add", DefaultIssuePromptPath)
				_, _ = c.runner.Run("git", "commit", "-m", "chore: round cap exceeded, handing over to human")
				_, _ = c.runner.Run("git", "push")
				return nil
			}

			// Re-dispatch corrector
			meta.Status = "running"
			if err := WritePlanMeta(DefaultIssuePromptPath, meta); err != nil {
				return err
			}
			_, _ = c.runner.Run("git", "add", DefaultIssuePromptPath)
			commitMsg := fmt.Sprintf("chore: changes requested, re-dispatching to corrector [round %d]", meta.Round)
			_, _ = c.runner.Run("git", "commit", "-m", commitMsg)
			_, _ = c.runner.Run("git", "push")
			return nil
		}

		return nil

	case "publish":
		// publish is run when merged -> STATUS == review and PLAN.md present
		if strings.ToLower(meta.Status) != "review" {
			return fmt.Errorf("cannot publish: STATUS is %q (expected review)", meta.Status)
		}
		_, err := MergeAndPublish(c.runner, c.publisher, "", "")
		return err

	default:
		return fmt.Errorf("unknown CI phase: %s", phase)
	}
}
