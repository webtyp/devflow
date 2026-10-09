package devflow_test

import (
	"testing"

	"webtyp.com/devflow"
)

func TestParseCLIArgs(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantMsg       string
		wantTag       string
		wantIsHelp    bool
		wantIsRelease bool
	}{
		{
			name:       "Help - help",
			args:       []string{"cmd", "help"},
			wantIsHelp: true,
		},
		{
			name:       "Help - -help",
			args:       []string{"cmd", "-help"},
			wantIsHelp: true,
		},
		{
			name:       "Help - --help",
			args:       []string{"cmd", "--help"},
			wantIsHelp: true,
		},
		{
			name:       "Help - h",
			args:       []string{"cmd", "h"},
			wantIsHelp: true,
		},
		{
			name:       "Help - -h",
			args:       []string{"cmd", "-h"},
			wantIsHelp: true,
		},
		{
			name:       "Help - ?",
			args:       []string{"cmd", "?"},
			wantIsHelp: true,
		},
		{
			name:       "Help - -?",
			args:       []string{"cmd", "-?"},
			wantIsHelp: true,
		},
		{
			name:          "Message only",
			args:          []string{"cmd", "feat: something"},
			wantMsg:       "feat: something",
			wantTag:       "",
			wantIsHelp:    false,
			wantIsRelease: false,
		},
		{
			name:          "Message and tag",
			args:          []string{"cmd", "feat: something", "v1.2.3"},
			wantMsg:       "feat: something",
			wantTag:       "v1.2.3",
			wantIsHelp:    false,
			wantIsRelease: false,
		},
		{
			name:          "Empty args",
			args:          []string{"cmd"},
			wantMsg:       "",
			wantTag:       "",
			wantIsHelp:    false,
			wantIsRelease: false,
		},
		{
			name:          "Message with -release flag",
			args:          []string{"cmd", "feat: something", "-release"},
			wantMsg:       "feat: something",
			wantTag:       "",
			wantIsHelp:    false,
			wantIsRelease: true,
		},
		{
			name:          "Message, tag, and -release flag",
			args:          []string{"cmd", "feat: something", "v1.2.3", "-release"},
			wantMsg:       "feat: something",
			wantTag:       "v1.2.3",
			wantIsHelp:    false,
			wantIsRelease: true,
		},
		{
			name:          "-release at different position",
			args:          []string{"cmd", "-release", "feat: something", "v1.2.3"},
			wantMsg:       "-release",
			wantTag:       "feat: something",
			wantIsHelp:    false,
			wantIsRelease: true,
		},
		{
			name:          "--release flag variant",
			args:          []string{"cmd", "feat: something", "--release"},
			wantMsg:       "feat: something",
			wantTag:       "",
			wantIsHelp:    false,
			wantIsRelease: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, tag, isHelp, isRelease := devflow.ParseCLIArgs(tt.args)
			if msg != tt.wantMsg {
				t.Errorf("ParseCLIArgs() msg = %v, want %v", msg, tt.wantMsg)
			}
			if tag != tt.wantTag {
				t.Errorf("ParseCLIArgs() tag = %v, want %v", tag, tt.wantTag)
			}
			if isHelp != tt.wantIsHelp {
				t.Errorf("ParseCLIArgs() isHelp = %v, want %v", isHelp, tt.wantIsHelp)
			}
			if isRelease != tt.wantIsRelease {
				t.Errorf("ParseCLIArgs() isRelease = %v, want %v", isRelease, tt.wantIsRelease)
			}
		})
	}
}

func TestParseCLIArgs_NoCascadeFlag(t *testing.T) {
	// Simulated main logic for flag filtering
	filter := func(args []string) (bool, []string) {
		var noCascade bool
		filtered := []string{args[0]}
		for _, arg := range args[1:] {
			if arg == "--no-cascade" {
				noCascade = true
			} else {
				filtered = append(filtered, arg)
			}
		}
		return noCascade, filtered
	}

	args := []string{"gopush", "feat: test", "--no-cascade"}
	noCascade, filtered := filter(args)
	if !noCascade {
		t.Fatal("expected noCascade to be true")
	}
	msg, _, _, _ := devflow.ParseCLIArgs(filtered)
	if msg != "feat: test" {
		t.Errorf("expected message 'feat: test', got %q", msg)
	}

	// Absent case
	args = []string{"gopush", "feat: test"}
	noCascade, _ = filter(args)
	if noCascade {
		t.Fatal("expected noCascade to be false")
	}
}

func TestParseArgs_CIPhases(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantCIPhase string
		wantCmd     string
	}{
		{
			name:        "--ci dispatch as separate arg",
			args:        []string{"cmd", "--ci", "dispatch"},
			wantCIPhase: "dispatch",
		},
		{
			name:        "--ci review as separate arg",
			args:        []string{"cmd", "--ci", "review"},
			wantCIPhase: "review",
		},
		{
			name:        "--ci verdict as separate arg",
			args:        []string{"cmd", "--ci", "verdict"},
			wantCIPhase: "verdict",
		},
		{
			name:        "--ci publish as separate arg",
			args:        []string{"cmd", "--ci", "publish"},
			wantCIPhase: "publish",
		},
		{
			name:        "--ci=<phase> inline form",
			args:        []string{"cmd", "--ci=dispatch"},
			wantCIPhase: "dispatch",
		},
		{
			name:    "no --ci flag leaves CIPhase empty",
			args:    []string{"cmd", "dispatch"},
			wantCmd: "dispatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := devflow.ParseCodeJobFlags(tt.args)
			if opts.CIPhase != tt.wantCIPhase {
				t.Errorf("ParseCodeJobFlags() CIPhase = %q, want %q", opts.CIPhase, tt.wantCIPhase)
			}
			if opts.Command != tt.wantCmd {
				t.Errorf("ParseCodeJobFlags() Command = %q, want %q", opts.Command, tt.wantCmd)
			}
		})
	}
}

func TestParseArgs_InitFlags(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantInitAction bool
		wantForce      bool
		wantOrg        string
		wantVisibility string
	}{
		{
			name:           "--init-action alone",
			args:           []string{"cmd", "--init-action"},
			wantInitAction: true,
		},
		{
			name:           "--init-action --force",
			args:           []string{"cmd", "--init-action", "--force"},
			wantInitAction: true,
			wantForce:      true,
		},
		{
			name:           "--init-action --org as separate arg",
			args:           []string{"cmd", "--init-action", "--org", "myorg"},
			wantInitAction: true,
			wantOrg:        "myorg",
		},
		{
			name:           "--init-action --org=<name> inline form",
			args:           []string{"cmd", "--init-action", "--org=myorg"},
			wantInitAction: true,
			wantOrg:        "myorg",
		},
		{
			name:           "--visibility as separate arg",
			args:           []string{"cmd", "--init-action", "--org", "myorg", "--visibility", "private"},
			wantInitAction: true,
			wantOrg:        "myorg",
			wantVisibility: "private",
		},
		{
			name:           "--visibility=<v> inline form",
			args:           []string{"cmd", "--init-action", "--visibility=all"},
			wantInitAction: true,
			wantVisibility: "all",
		},
		{
			name: "no init flags",
			args: []string{"cmd", "dispatch"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := devflow.ParseCodeJobFlags(tt.args)
			if opts.InitAction != tt.wantInitAction {
				t.Errorf("ParseCodeJobFlags() InitAction = %v, want %v", opts.InitAction, tt.wantInitAction)
			}
			if opts.Force != tt.wantForce {
				t.Errorf("ParseCodeJobFlags() Force = %v, want %v", opts.Force, tt.wantForce)
			}
			if opts.Org != tt.wantOrg {
				t.Errorf("ParseCodeJobFlags() Org = %q, want %q", opts.Org, tt.wantOrg)
			}
			if opts.Visibility != tt.wantVisibility {
				t.Errorf("ParseCodeJobFlags() Visibility = %q, want %q", opts.Visibility, tt.wantVisibility)
			}
		})
	}
}

func TestParseCodeJobFlags_ExplicitCommands(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantCommand   string
		wantMessage   string
		wantTag       string
		wantReplyText string
		wantError     string
	}{
		{
			name:        "Bare dispatch",
			args:        []string{"codejob", "dispatch"},
			wantCommand: "dispatch",
		},
		{
			name:        "Dispatch with extra arg",
			args:        []string{"codejob", "dispatch", "foo"},
			wantCommand: "dispatch",
			wantError:   "codejob: unknown command \"foo\"; run codejob for help",
		},
		{
			name:        "Bare pull",
			args:        []string{"codejob", "pull"},
			wantCommand: "pull",
		},
		{
			name:        "Bare reply (error)",
			args:        []string{"codejob", "reply"},
			wantCommand: "reply",
			wantError:   "codejob: reply needs a message: codejob reply \"text\"",
		},
		{
			name:          "Reply with text",
			args:          []string{"codejob", "reply", "some message"},
			wantCommand:   "reply",
			wantReplyText: "some message",
		},
		{
			name:          "Reply with text and extra arg",
			args:          []string{"codejob", "reply", "some message", "foo"},
			wantCommand:   "reply",
			wantReplyText: "some message",
			wantError:     "codejob: unknown command \"foo\"; run codejob for help",
		},
		{
			name:        "Bare approve",
			args:        []string{"codejob", "approve"},
			wantCommand: "approve",
		},
		{
			name:        "Bare close (error)",
			args:        []string{"codejob", "close"},
			wantCommand: "close",
			wantError:   "codejob: close needs a commit message: codejob close \"message\" [tag]",
		},
		{
			name:        "Close with message",
			args:        []string{"codejob", "close", "chore: xyz"},
			wantCommand: "close",
			wantMessage: "chore: xyz",
		},
		{
			name:        "Close with message and tag",
			args:        []string{"codejob", "close", "chore: xyz", "v1.0.0"},
			wantCommand: "close",
			wantMessage: "chore: xyz",
			wantTag:     "v1.0.0",
		},
		{
			name:        "Close with message, tag, and extra arg",
			args:        []string{"codejob", "close", "chore: xyz", "v1.0.0", "foo"},
			wantCommand: "close",
			wantMessage: "chore: xyz",
			wantTag:     "v1.0.0",
			wantError:   "codejob: unknown command \"foo\"; run codejob for help",
		},
		{
			name:      "Unknown command",
			args:      []string{"codejob", "unknowncmd"},
			wantError: "codejob: unknown command \"unknowncmd\"; run codejob for help",
		},
		{
			name:      "Positional message no longer works",
			args:      []string{"codejob", "fix: everything"},
			wantError: "codejob: unknown command \"fix: everything\"; run codejob for help",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := devflow.ParseCodeJobFlags(tt.args)
			if opts.Command != tt.wantCommand {
				t.Errorf("expected Command %q, got %q", tt.wantCommand, opts.Command)
			}
			if opts.Message != tt.wantMessage {
				t.Errorf("expected Message %q, got %q", tt.wantMessage, opts.Message)
			}
			if opts.Tag != tt.wantTag {
				t.Errorf("expected Tag %q, got %q", tt.wantTag, opts.Tag)
			}
			if opts.ReplyText != tt.wantReplyText {
				t.Errorf("expected ReplyText %q, got %q", tt.wantReplyText, opts.ReplyText)
			}
			if opts.ParseError != tt.wantError {
				t.Errorf("expected ParseError %q, got %q", tt.wantError, opts.ParseError)
			}
		})
	}
}

func TestParseCodeJobFlags_ReleaseFlag(t *testing.T) {
	opts := devflow.ParseCodeJobFlags([]string{"codejob", "--release", "close", "msg"})
	if !opts.IsRelease {
		t.Errorf("expected IsRelease true")
	}
	if opts.ParseError != "" {
		t.Errorf("unexpected error: %s", opts.ParseError)
	}

	opts = devflow.ParseCodeJobFlags([]string{"codejob", "--release", "pull"})
	if opts.ParseError != "codejob: --release can only be used with the close command" {
		t.Errorf("expected specific release error, got %q", opts.ParseError)
	}

	opts = devflow.ParseCodeJobFlags([]string{"codejob", "--release"})
	if opts.ParseError != "codejob: --release can only be used with the close command" {
		t.Errorf("expected specific release error for bare --release, got %q", opts.ParseError)
	}
}
