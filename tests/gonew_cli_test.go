package devflow_test

import (
	"errors"
	"flag"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

func TestParseAddRemoteArgsFlagsAfterPath(t *testing.T) {
	// Regression: gonew add-remote ./my-project -owner=veltylabs was ignoring
	// flags after the positional path because flag.Parse stops at the first
	// non-flag argument.
	opts, err := devflow.ParseAddRemoteArgs([]string{"./my-project", "-owner=veltylabs", "-visibility=private"})
	if err != nil {
		t.Fatalf("ParseAddRemoteArgs failed: %v", err)
	}
	if opts.ProjectPath != "./my-project" {
		t.Errorf("ProjectPath = %q, want %q", opts.ProjectPath, "./my-project")
	}
	if opts.Owner != "veltylabs" {
		t.Errorf("Owner = %q, want %q", opts.Owner, "veltylabs")
	}
	if opts.Visibility != "private" {
		t.Errorf("Visibility = %q, want %q", opts.Visibility, "private")
	}
}

func TestParseAddRemoteArgsFlagsBeforePath(t *testing.T) {
	opts, err := devflow.ParseAddRemoteArgs([]string{"-owner=veltylabs", "-visibility=private", "./my-project"})
	if err != nil {
		t.Fatalf("ParseAddRemoteArgs failed: %v", err)
	}
	if opts.ProjectPath != "./my-project" || opts.Owner != "veltylabs" || opts.Visibility != "private" {
		t.Errorf("unexpected result: %+v", opts)
	}
}

func TestParseAddRemoteArgsSpaceSeparated(t *testing.T) {
	opts, err := devflow.ParseAddRemoteArgs([]string{"./my-project", "-owner", "veltylabs", "-visibility", "private"})
	if err != nil {
		t.Fatalf("ParseAddRemoteArgs failed: %v", err)
	}
	if opts.ProjectPath != "./my-project" || opts.Owner != "veltylabs" || opts.Visibility != "private" {
		t.Errorf("unexpected result: %+v", opts)
	}
}

func TestParseAddRemoteArgsDefaults(t *testing.T) {
	opts, err := devflow.ParseAddRemoteArgs([]string{"./my-project", "--owner=veltylabs"})
	if err != nil {
		t.Fatalf("ParseAddRemoteArgs failed: %v", err)
	}
	if opts.ProjectPath != "./my-project" {
		t.Errorf("ProjectPath = %q, want %q", opts.ProjectPath, "./my-project")
	}
	if opts.Visibility != "public" {
		t.Errorf("Visibility = %q, want %q", opts.Visibility, "public")
	}
}

func TestParseAddRemoteArgsNoPath(t *testing.T) {
	if _, err := devflow.ParseAddRemoteArgs(nil); err == nil {
		t.Error("expected error for missing project path")
	}
	if _, err := devflow.ParseAddRemoteArgs([]string{"-owner=veltylabs"}); err == nil {
		t.Error("expected error when only flags are given")
	}
}

func TestParseGoNewArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want devflow.GoNewCLIOpts
	}{
		{
			name: "Name and description only",
			args: []string{"my-repo", "A sample Go project", "--owner=webtyp"},
			want: devflow.GoNewCLIOpts{Name: "my-repo", Description: "A sample Go project", Owner: "webtyp", Visibility: "public", License: "MIT"},
		},
		{
			name: "Flags after positional args",
			args: []string{"my-repo", "A sample Go project", "-owner=cdvelop", "-visibility=private", "-local-only"},
			want: devflow.GoNewCLIOpts{Name: "my-repo", Description: "A sample Go project", Owner: "cdvelop", Visibility: "private", LocalOnly: true, License: "MIT"},
		},
		{
			name: "Long form flags before positional",
			args: []string{"--owner=veltylabs", "--license=Apache-2.0", "my-repo", "A sample Go project"},
			want: devflow.GoNewCLIOpts{Name: "my-repo", Description: "A sample Go project", Owner: "veltylabs", Visibility: "public", License: "Apache-2.0"},
		},
		{
			name: "Space separated flag values",
			args: []string{"my-repo", "A sample Go project", "-owner", "webtyp"},
			want: devflow.GoNewCLIOpts{Name: "my-repo", Description: "A sample Go project", Owner: "webtyp", Visibility: "public", License: "MIT"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := devflow.ParseGoNewArgs(tt.args)
			if err != nil {
				t.Fatalf("ParseGoNewArgs failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseGoNewArgs(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseGoNewArgsInsufficientArgs(t *testing.T) {
	for _, args := range [][]string{nil, {"my-repo"}} {
		if _, err := devflow.ParseGoNewArgs(args); err == nil {
			t.Errorf("ParseGoNewArgs(%q) expected error, got nil", args)
		}
	}
}

func TestParseArgsOwnerIsRequired(t *testing.T) {
	if _, err := devflow.ParseGoNewArgs([]string{"my-repo", "A sample Go project"}); err != devflow.ErrOwnerRequired {
		t.Errorf("ParseGoNewArgs without owner: got %v, want ErrOwnerRequired", err)
	}
	if _, err := devflow.ParseAddRemoteArgs([]string{"./my-project"}); err != devflow.ErrOwnerRequired {
		t.Errorf("ParseAddRemoteArgs without owner: got %v, want ErrOwnerRequired", err)
	}
}

func TestParseArgsHelpTokens(t *testing.T) {
	for _, tok := range []string{"help", "--help", "-help", "-h", "h", "?", "-?"} {
		if _, err := devflow.ParseGoNewArgs([]string{tok}); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("ParseGoNewArgs(%q) err = %v, want flag.ErrHelp", tok, err)
		}
		if _, err := devflow.ParseAddRemoteArgs([]string{tok}); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("ParseAddRemoteArgs(%q) err = %v, want flag.ErrHelp", tok, err)
		}
	}
}

func TestGoNewBinaryAlwaysShowsHelp(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "gonew")
	build := exec.Command("go", "build", "-o", bin, "../cmd/gonew")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	helpCases := [][]string{
		nil,
		{"--help"},
		{"-h"},
		{"-help"},
		{"help"},
		{"help", "anything"},
		{"?"},
		{"foo"},
		{"--unknown"},
		{"add-remote"},
		{"add-remote", "--help"},
		{"add-remote", "--unknown"},
	}

	for _, args := range helpCases {
		out, err := exec.Command(bin, args...).CombinedOutput()
		if err != nil {
			t.Errorf("gonew %v exited with error: %v\n%s", args, err, out)
			continue
		}
		s := string(out)
		if strings.Contains(s, "Error:") {
			t.Errorf("gonew %v printed unexpected Error:\n%s", args, s)
		}
		if !strings.Contains(s, "Usage:") {
			t.Errorf("gonew %v did not print usage help:\n%s", args, s)
		}
	}

	// Missing --owner still reports error AND shows help.
	out, err := exec.Command(bin, "my-repo", "desc").CombinedOutput()
	if err == nil {
		t.Errorf("gonew my-repo desc expected non-zero exit when --owner is missing")
	}
	s := string(out)
	if !strings.Contains(s, "Error:") || !strings.Contains(s, "Usage:") {
		t.Errorf("gonew my-repo desc expected Error and Usage, got:\n%s", s)
	}
}
