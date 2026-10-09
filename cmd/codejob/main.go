package main

import (
	"fmt"
	"os"
	"strings"
	gitmod "webtyp.com/git"

	"webtyp.com/devflow"
	"webtyp.com/gorelease"
	keyring "webtyp.com/keyring/auto"
)

func main() {
	opts := devflow.ParseCodeJobFlags(os.Args)

	if opts.ParseError != "" {
		fmt.Fprintln(os.Stderr, opts.ParseError)
		os.Exit(2)
	}

	if opts.IsHelp {
		showHelp()
		return
	}

	if opts.IsResetGHToken {
		kr, err := keyring.NewKeyring("devflow")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		if err := gitmod.NewGitHubPATAuth(kr).Reset(); err != nil {
			fmt.Fprintln(os.Stderr, "Error resetting GitHub token:", err)
			os.Exit(1)
		}
		fmt.Println("GitHub token reset successfully.")
		return
	}

	if opts.InitAction {
		if err := devflow.InitCodejobAction(opts.Force, opts.Org, opts.Visibility); err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing action:", err)
			os.Exit(1)
		}
		return
	}

	if opts.Command == "reply" || opts.Command == "approve" {
		job := devflow.NewCodeJob(devflow.NewJulesDriver(devflow.JulesConfig{}))
		var (
			out string
			err error
		)
		if opts.Command == "approve" {
			out, err = job.Approve()
		} else {
			out, err = job.Reply(opts.ReplyText)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println(out)
		return
	}

	if opts.Command == "" && !devflow.IsEnvironmentValid(".env") {
		showHelp()
		return
	}

	git, err := gitmod.NewGit()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	goHandler, err := devflow.NewGo(git)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	log := func(args ...any) { fmt.Println(args...) }
	goHandler.SetLog(log)
	goHandler.SetConsoleOutput(func(s string) { fmt.Println(s) })

	// Ensure gh session is valid before creating the GitHub handler
	// to prevent the interactive device flow from triggering early.
	if err := gitmod.EnsureGHSession(gitmod.RealRunner{}, keyring.OpenKeyring("devflow")); err != nil {
		fmt.Fprintln(os.Stderr, "GitHub session error:", err)
		os.Exit(1)
	}

	patAuth := gitmod.NewGitHubPATAuth(keyring.OpenKeyring("devflow"))
	gh, err := gitmod.NewGitHub(log, nil, patAuth)
	if err != nil {
		fmt.Fprintln(os.Stderr, "GitHub error:", err)
		os.Exit(1)
	}

	job := devflow.NewCodeJob(devflow.NewJulesDriver(devflow.JulesConfig{}))
	job.SetLog(log)
	job.SetPublisher(goHandler)

	// Inject the release function if -release flag is used
	if opts.IsRelease {
		rel, err := gorelease.New(git)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		rel.SetLog(log)
		rel.SetConsoleOutput(func(s string) { fmt.Println(s) })
		job.SetReleaser(func(releaseTag string) error {
			return rel.ReleaseOnly(releaseTag, gh)
		})
	}

	if opts.CIPhase != "" {
		if err := job.RunCI(opts.CIPhase); err != nil {
			fmt.Fprintln(os.Stderr, "CI Phase Error:", err)
			os.Exit(1)
		}
		return
	}

	var result string
	switch opts.Command {
	case "dispatch":
		result, err = job.Dispatch()
	case "pull":
		result, err = job.Pull()
	case "close":
		result, err = job.Close(opts.Message, opts.Tag, opts.IsRelease)
	case "":
		// bare codejob: print status line and exit 0
		result, err = job.StatusLine()
	default:
		// Should be caught by parsing, but fallback
		err = fmt.Errorf("unknown command: %s", opts.Command)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	// Format output: "jules: <id>" -> "Agent Jules • Session: <id>"
	if strings.HasPrefix(result, devflow.JulesResultPrefix) {
		sessionID := strings.TrimPrefix(result, devflow.JulesResultPrefix)
		fmt.Printf("Agent Jules • Session: %s\n", sessionID)
	} else if result != "" {
		fmt.Println(result)
	}
}

func showHelp() {
	fmt.Println("Usage: codejob [command] [flags]")
	fmt.Println("\nCommands:")
	fmt.Println("  (no command)         Read-only status of the current plan")
	fmt.Println("  dispatch             Send docs/PLAN.md to the AI agent")
	fmt.Println("  pull                 Check status or fetch/fast-forward the agent's PR branch")
	fmt.Println("  reply \"text\"         Answer the agent session of docs/PLAN.md")
	fmt.Println("  approve              Approve the plan that the session is waiting on")
	fmt.Println("  close \"msg\" [tag]    Merge the PR, publish the module, and delete the plan")
	fmt.Println("\nFlags:")
	fmt.Println("  --release            Create a GitHub Release after merge and publish (with close only)")
	fmt.Println("  --reset-gh-token     Remove the stored GitHub PAT from the keyring")
	fmt.Println("  --ci <phase>         Run a single CI state transition:")
	fmt.Println("                       dispatch | review | verdict | publish")
	fmt.Println("  --init-action        Scaffold .github/workflows/codejob.yml and register secrets")
	fmt.Println("  --force              With --init-action, overwrite an existing workflow file")
	fmt.Println("  --org <name>         With --init-action, register secrets at the org level")
	fmt.Println("  --visibility <v>     With --init-action --org, secret visibility (all|private|selected)")
	fmt.Println("\nHelp Commands:")
	fmt.Println("  help, --help, -help, -h, h, ?, -?    Show this help message")
	fmt.Println("\nDescription:")
	fmt.Println("  CodeJob orchestrates coding tasks by sending instructions to AI agents.")
	fmt.Println("  All state lives in the frontmatter of docs/PLAN.md, so the loop (dispatch")
	fmt.Println("  → review → publish) can run locally or entirely in GitHub Actions.")
	fmt.Println("\nWorkflow:")
	fmt.Printf("  1. DISPATCH: Create %s and run 'codejob dispatch' to start a new task.\n", devflow.DefaultIssuePromptPath)
	fmt.Println("               STATUS: dispatch is written to the PLAN.md frontmatter.")
	fmt.Println("  2. REVIEW:   Run 'codejob pull' from THIS SAME local repo —")
	fmt.Println("               do not clone the repo elsewhere or 'gh pr checkout' by hand.")
	fmt.Println("               Once the agent's PR is ready, pulling moves STATUS to review")
	fmt.Println("               (or reviewing if a REVIEWER is set) and checks out the PR")
	fmt.Println("               branch IN PLACE for local inspection.")
	fmt.Println("               If Jules stopped (a question, a plan to approve, or finished")
	fmt.Println("               without a PR), answer with 'codejob reply \"...\"' or 'codejob approve'.")
	fmt.Println("  3. RESOLVE:")
	fmt.Println("     - APPROVE: Run 'codejob close \"message\"' to merge the PR and publish;")
	fmt.Println("                docs/PLAN.md is deleted once published.")
	fmt.Println("     - ITERATE: If adjustments are needed, create a new docs/PLAN.md and run")
	fmt.Println("                'codejob dispatch'. The old PR is merged first, then the new plan is dispatched.")
	fmt.Println("\nNote: every step above runs from the SAME local clone you dispatched from.")
	fmt.Println("codejob tracks state in that repo's docs/PLAN.md and checks out branches in")
	fmt.Println("place — there is never a reason to 'gh repo clone' or 'gh pr checkout' a repo")
	fmt.Println("codejob already manages.")
}
