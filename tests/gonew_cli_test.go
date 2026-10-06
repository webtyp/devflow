package devflow_test

import (
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
	opts, err := devflow.ParseAddRemoteArgs([]string{"./my-project"})
	if err != nil {
		t.Fatalf("ParseAddRemoteArgs failed: %v", err)
	}
	if opts.ProjectPath != "./my-project" {
		t.Errorf("ProjectPath = %q, want %q", opts.ProjectPath, "./my-project")
	}
	if opts.Owner != "" {
		t.Errorf("Owner = %q, want empty", opts.Owner)
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
			args: []string{"my-repo", "A sample Go project"},
			want: devflow.GoNewCLIOpts{Name: "my-repo", Description: "A sample Go project", Visibility: "public", License: "MIT"},
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
