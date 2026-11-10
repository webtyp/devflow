package devflow

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// GoNewCLIOpts holds parsed options for the gonew create CLI.
type GoNewCLIOpts struct {
	Name        string
	Description string
	Owner       string
	Visibility  string
	LocalOnly   bool
	License     string
}

// AddRemoteCLIOpts holds parsed options for the gonew add-remote CLI.
type AddRemoteCLIOpts struct {
	ProjectPath string
	Owner       string
	Visibility  string
}

// ParseGoNewArgs parses `gonew <repo-name> <description> [flags]`.
// Flags may appear before or after the positional arguments.
func ParseGoNewArgs(args []string) (GoNewCLIOpts, error) {
	fs := flag.NewFlagSet("gonew", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	owner := fs.String("owner", "", "GitHub owner/organization (required)")
	visibility := fs.String("visibility", "public", "Visibility (public/private)")
	localOnly := fs.Bool("local-only", false, "Skip remote creation entirely")
	license := fs.String("license", "MIT", "License type (default: MIT)")

	reorderedArgs, positional := splitFlags(args, map[string]bool{
		"owner":      true,
		"visibility": true,
		"license":    true,
	})
	if err := fs.Parse(reorderedArgs); err != nil {
		return GoNewCLIOpts{}, err
	}
	if len(positional) < 2 {
		return GoNewCLIOpts{}, fmt.Errorf("usage: gonew <repo-name> <description> --owner <owner> [flags]")
	}
	if *owner == "" {
		return GoNewCLIOpts{}, ErrOwnerRequired
	}

	return GoNewCLIOpts{
		Name:        positional[0],
		Description: positional[1],
		Owner:       *owner,
		Visibility:  *visibility,
		LocalOnly:   *localOnly,
		License:     *license,
	}, nil
}

// ParseAddRemoteArgs parses `gonew add-remote <project-path> [flags]`.
func ParseAddRemoteArgs(args []string) (AddRemoteCLIOpts, error) {
	fs := flag.NewFlagSet("add-remote", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	owner := fs.String("owner", "", "GitHub owner/organization (required)")
	visibility := fs.String("visibility", "public", "Visibility (public/private)")

	reorderedArgs, positional := splitFlags(args, map[string]bool{
		"owner":      true,
		"visibility": true,
	})
	if err := fs.Parse(reorderedArgs); err != nil {
		return AddRemoteCLIOpts{}, err
	}
	if len(positional) < 1 {
		return AddRemoteCLIOpts{}, fmt.Errorf("usage: gonew add-remote <project-path> --owner <owner> [flags]")
	}
	if *owner == "" {
		return AddRemoteCLIOpts{}, ErrOwnerRequired
	}

	return AddRemoteCLIOpts{
		ProjectPath: positional[0],
		Owner:       *owner,
		Visibility:  *visibility,
	}, nil
}

// splitFlags reorders args so flags come before positional arguments.
// Go's flag package stops parsing at the first positional argument, so flags
// placed after positional args would otherwise be silently ignored.
// valueFlags lists flags that consume the next token as their value; the
// -flag=value form is passed through untouched.
func splitFlags(args []string, valueFlags map[string]bool) (flags, positional []string) {
	skipNext := false
	for i := 0; i < len(args); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		arg := args[i]
		if len(arg) > 0 && arg[0] == '-' {
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			if valueFlags[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				skipNext = true
			}
		} else {
			positional = append(positional, arg)
		}
	}
	return flags, positional
}
