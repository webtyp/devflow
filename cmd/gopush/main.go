package main

import (
	"fmt"
	gitmod "webtyp.com/git"
	"os"

	"webtyp.com/devflow"
	keyring "webtyp.com/keyring/auto"
)

func main() {
	usage := func() {
		fmt.Fprintf(os.Stderr, `gopush - Complete Go project workflow: test + git push + update dependents

Usage:
    gopush 'commit message' [tag]

Arguments:
    message    Commit message (required)
    tag        Tag name (optional, auto-generated if not provided)

Examples:
    gopush 'feat: new feature'
    gopush 'fix: bug' 'v1.2.3'

Flags:
    --no-cascade   Publish this module only; do not update dependent modules

`)
	}

	// Pre-process flags to keep positional args consistent
	var skipRace bool
	var noCascade bool
	filteredArgs := []string{os.Args[0]}
	for _, arg := range os.Args[1:] {
		if arg == "--skip-race" || arg == "-R" {
			skipRace = true
		} else if arg == "--no-cascade" {
			noCascade = true
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}

	message, tag, isHelp, _ := devflow.ParseCLIArgs(filteredArgs)

	if isHelp || (len(filteredArgs) == 1 && !devflow.IsEnvironmentValid(".env")) {
		usage()
		os.Exit(0)
	}

	// Message is mandatory if not in an active codejob session
	if message == "" && !devflow.IsEnvironmentValid(".env") {
		usage()
		os.Exit(0)
	}

	git, err := gitmod.NewGit()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	auth := gitmod.NewGitHubOAuth()
	kr, err := keyring.NewKeyring("devflow")
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	auth.SetStore(kr)
	git.SetAuthRetrier(auth)

	goHandler, err := devflow.NewGo(git)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	goHandler.SetSumDBClient(&gitmod.HTTPSumDB{})

	// Run Push with parsed options
	summary, err := goHandler.Push(message, tag, false, skipRace, noCascade, false, false, false, "..")
	if err != nil {
		fmt.Println("Push failed:", err)
		os.Exit(1)
	}

	fmt.Println(summary.Summary)
}
