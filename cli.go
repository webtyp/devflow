package devflow

import "strings"

// ParseCLIArgs parses command line arguments for devflow tools (codejob, gopush).
// It returns the message, tag, whether help was requested, and whether -release flag is present.
// Flags like -release and --release are detected and excluded from message/tag assignment.
func ParseCLIArgs(args []string) (message, tag string, isHelp, isRelease bool) {
	if len(args) > 1 {
		arg := strings.ToLower(args[1])
		switch arg {
		case "help", "-help", "--help", "h", "-h", "?", "-?":
			return "", "", true, false
		}
		message = args[1]
	}
	if len(args) > 2 {
		arg := args[2]
		// Don't assign -release or --release as tag
		if arg != "-release" && arg != "--release" {
			tag = arg
		}
	}
	// Scan all args for -release or --release flag
	for _, arg := range args[1:] {
		if arg == "-release" || arg == "--release" {
			isRelease = true
			break
		}
	}
	return
}

// CodeJobCLIOpts holds parsed options for the codejob CLI.
type CodeJobCLIOpts struct {
	Message        string
	Tag            string
	IsHelp         bool
	IsRelease      bool
	IsResetGHToken bool
	CIPhase        string // "dispatch", "review", "verdict", "publish"
	InitAction     bool
	Force          bool
	Org            string
	Visibility     string
	Reply          string // --reply "text": answer the Jules session of docs/PLAN.md
	Approve        bool   // --approve: approve the plan that session is waiting on
}

// ParseCodeJobFlags parses the complete set of flags and positional arguments for the codejob CLI.
func ParseCodeJobFlags(args []string) CodeJobCLIOpts {
	var opts CodeJobCLIOpts
	var remaining []string

	if len(args) == 0 {
		return opts
	}

	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" || arg == "help" {
			opts.IsHelp = true
		} else if arg == "-release" || arg == "--release" {
			opts.IsRelease = true
		} else if arg == "--reset-gh-token" {
			opts.IsResetGHToken = true
		} else if arg == "--init-action" {
			opts.InitAction = true
		} else if arg == "--force" {
			opts.Force = true
		} else if strings.HasPrefix(arg, "--ci=") {
			opts.CIPhase = strings.TrimPrefix(arg, "--ci=")
		} else if arg == "--ci" && i+1 < len(args) {
			opts.CIPhase = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--org=") {
			opts.Org = strings.TrimPrefix(arg, "--org=")
		} else if arg == "--org" && i+1 < len(args) {
			opts.Org = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--reply=") {
			opts.Reply = strings.TrimPrefix(arg, "--reply=")
		} else if arg == "--reply" && i+1 < len(args) {
			opts.Reply = args[i+1]
			i++
		} else if arg == "--approve" {
			opts.Approve = true
		} else if strings.HasPrefix(arg, "--visibility=") {
			opts.Visibility = strings.TrimPrefix(arg, "--visibility=")
		} else if arg == "--visibility" && i+1 < len(args) {
			opts.Visibility = args[i+1]
			i++
		} else {
			remaining = append(remaining, arg)
		}
	}

	if len(remaining) > 0 {
		opts.Message = remaining[0]
	}
	if len(remaining) > 1 {
		opts.Tag = remaining[1]
	}

	return opts
}

// ParseCodeJobArgs parses codejob CLI: codejob [message] [tag] [--reset-gh-token]
// Returns message, tag, isHelp, isRelease, and isResetGHToken.
func ParseCodeJobArgs(args []string) (message, tag string, isHelp, isRelease, isResetGHToken bool) {
	opts := ParseCodeJobFlags(args)
	return opts.Message, opts.Tag, opts.IsHelp, opts.IsRelease, opts.IsResetGHToken
}
