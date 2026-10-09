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

const (
	cmdDispatch = "dispatch"
	cmdPull     = "pull"
	cmdReply    = "reply"
	cmdApprove  = "approve"
	cmdClose    = "close"
)

// CodeJobCLIOpts holds parsed options for the codejob CLI.
type CodeJobCLIOpts struct {
	Command        string // cmdDispatch, cmdPull, cmdReply, cmdApprove, cmdClose, or ""
	Message        string // only filled for close
	Tag            string // only filled for close
	ReplyText      string // only filled for reply
	ParseError     string // set if parsing fails
	IsHelp         bool
	IsRelease      bool // only valid with close
	IsResetGHToken bool
	CIPhase        string // "dispatch", "review", "verdict", "publish"
	InitAction     bool
	Force          bool
	Org            string
	Visibility     string
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
		} else if strings.HasPrefix(arg, "--visibility=") {
			opts.Visibility = strings.TrimPrefix(arg, "--visibility=")
		} else if arg == "--visibility" && i+1 < len(args) {
			opts.Visibility = args[i+1]
			i++
		} else {
			remaining = append(remaining, arg)
		}
	}

	if opts.IsHelp || opts.IsResetGHToken || opts.InitAction || opts.CIPhase != "" {
		// When flags like --ci or --init-action or --help are passed, we don't strictly require a command.
		// However, if there are remaining args, we shouldn't necessarily error out, as some tests pass --ci alongside message.
		// Actually, let's process remaining just in case.
	}

	if len(remaining) > 0 {
		cmd := remaining[0]
		switch cmd {
		case cmdDispatch, cmdPull, cmdApprove:
			opts.Command = cmd
			if len(remaining) > 1 {
				opts.ParseError = "codejob: unknown command \"" + strings.Join(remaining[1:], " ") + "\"; run codejob for help"
			}
		case cmdReply:
			opts.Command = cmd
			if len(remaining) > 1 {
				opts.ReplyText = remaining[1]
				if len(remaining) > 2 {
					opts.ParseError = "codejob: unknown command \"" + strings.Join(remaining[2:], " ") + "\"; run codejob for help"
				}
			} else {
				opts.ParseError = "codejob: reply needs a message: codejob reply \"text\""
			}
		case cmdClose:
			opts.Command = cmd
			if len(remaining) > 1 {
				opts.Message = remaining[1]
				if len(remaining) > 2 {
					opts.Tag = remaining[2]
				}
				if len(remaining) > 3 {
					opts.ParseError = "codejob: unknown command \"" + strings.Join(remaining[3:], " ") + "\"; run codejob for help"
				}
			} else {
				opts.ParseError = "codejob: close needs a commit message: codejob close \"message\" [tag]"
			}
		default:
			opts.ParseError = "codejob: unknown command \"" + cmd + "\"; run codejob for help"
		}
	}

	if opts.IsRelease && opts.Command != cmdClose && opts.Command != "" {
		opts.ParseError = "codejob: --release can only be used with the close command"
	}

	// Ensure bare commands don't have release flag if it's strictly enforced.
	if opts.IsRelease && opts.Command == "" && len(remaining) == 0 && !opts.IsHelp && !opts.InitAction && !opts.IsResetGHToken && opts.CIPhase == "" {
		opts.ParseError = "codejob: --release can only be used with the close command"
	}

	return opts
}
