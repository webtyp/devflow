package devflow_test

import (
	"strings"
	gitmod "webtyp.com/git"
)

// newTestGitHub creates a *GitHub with injected fakeRunner.
func newTestGitHub(fake *fakeRunner) *gitmod.GitHub {
	gh := &gitmod.GitHub{}
	gh.SecretRunner = fake
	return gh
}

type fakeRunner struct {
	lastArgs  []string
	lastInput string
	output    string
	err       error
	respond   func(args []string) (string, error)
}

func (f *fakeRunner) Run(name string, args ...string) (string, error) {
	f.lastArgs = args
	if f.respond != nil {
		return f.respond(args)
	}
	return f.output, f.err
}

func (f *fakeRunner) RunSilent(name string, args ...string) (string, error) {
	f.lastArgs = args
	if f.respond != nil {
		return f.respond(args)
	}
	return f.output, f.err
}

func (f *fakeRunner) RunWithStdin(input, name string, args ...string) (string, error) {
	f.lastInput = input
	f.lastArgs = args
	return f.output, f.err
}



type mockRunner struct {
	calls  []string
	result string
}

func (m *mockRunner) Run(name string, args ...string) (string, error) {
	m.calls = append(m.calls, name+" "+strings.Join(args, " "))
	if len(args) > 0 && args[0] == "pr" && len(args) > 1 && args[1] == "view" && strings.Contains(strings.Join(args, " "), "reviews") {
		return m.result, nil
	}
	if len(args) > 0 && args[0] == "pr" {
		return "feat-branch", nil
	}
	if name == "git" && len(args) > 0 && args[0] == "branch" {
		return "feat-branch", nil
	}
	if name == "git" && len(args) > 0 && args[0] == "rev-list" {
		return "0", nil
	}
	return "ok", nil
}
