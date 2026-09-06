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
		wantMsg     string
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
			args:    []string{"cmd", "feat: something"},
			wantMsg: "feat: something",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := devflow.ParseCodeJobFlags(tt.args)
			if opts.CIPhase != tt.wantCIPhase {
				t.Errorf("ParseCodeJobFlags() CIPhase = %q, want %q", opts.CIPhase, tt.wantCIPhase)
			}
			if opts.Message != tt.wantMsg {
				t.Errorf("ParseCodeJobFlags() Message = %q, want %q", opts.Message, tt.wantMsg)
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
			args: []string{"cmd", "feat: something"},
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

