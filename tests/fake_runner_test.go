package devflow_test

import gitmod "webtyp.com/git"

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
