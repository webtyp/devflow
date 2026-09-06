package main

import (
	"fmt"
	gitmod "webtyp.com/git"
	"os"

	"webtyp.com/devflow"
	keyring "webtyp.com/keyring/auto"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "add-remote" {
		opts, err := devflow.ParseAddRemoteArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		handleAddRemote(opts)
		return
	}

	opts, err := devflow.ParseGoNewArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	handleCreate(opts)
}

func handleCreate(opts devflow.GoNewCLIOpts) {
	git, err := gitmod.NewGit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	log := func(args ...any) { fmt.Println(args...) }

	var githubFuture *devflow.Future
	if !opts.LocalOnly {
		githubFuture = devflow.NewFuture(func() (any, error) {
			kr, err := keyring.NewKeyring("devflow")
			if err != nil {
				return nil, err
			}
			return gitmod.NewGitHub(log, kr)
		})
	}

	goHandler, err := devflow.NewGo(git)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	orchestrator := devflow.NewGoNew(git, githubFuture, goHandler)

	summary, err := orchestrator.Create(devflow.NewProjectOptions{
		Name:        opts.Name,
		Description: opts.Description,
		Owner:       opts.Owner,
		Visibility:  opts.Visibility,
		LocalOnly:   opts.LocalOnly,
		License:     opts.License,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(summary)
}

func handleAddRemote(opts devflow.AddRemoteCLIOpts) {
	git, err := gitmod.NewGit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	log := func(args ...any) { fmt.Println(args...) }

	githubFuture := devflow.NewFuture(func() (any, error) {
		kr, err := keyring.NewKeyring("devflow")
		if err != nil {
			return nil, err
		}
		return gitmod.NewGitHub(log, kr)
	})

	goHandler, err := devflow.NewGo(git)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	orchestrator := devflow.NewGoNew(git, githubFuture, goHandler)

	summary, err := orchestrator.AddRemote(opts.ProjectPath, opts.Visibility, opts.Owner)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(summary)
}
