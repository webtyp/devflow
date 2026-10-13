package devflow_test

import (
	"os"
	"strings"
	"testing"

	gitmod "webtyp.com/git"

	"webtyp.com/devflow"
)

// planIn writes docs/PLAN.md with the given frontmatter lines in a fresh temp dir and chdirs there.
func planIn(t *testing.T, frontmatter string) {
	t.Helper()
	dir := t.TempDir()
	restore := testChdir(t, dir)
	t.Cleanup(restore)
	if err := os.MkdirAll("docs", 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nPLAN: \"feat: x\"\n" + frontmatter + "---\nbody\n"
	if err := os.WriteFile(devflow.DefaultIssuePromptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// commitMessages returns the -m argument of every "git commit" call recorded by m.
func commitMessages(calls []string) []string {
	var out []string
	for _, c := range calls {
		if i := strings.Index(c, "git commit -m "); i >= 0 {
			out = append(out, c[i+len("git commit -m "):])
		}
	}
	return out
}

// Every commit codejob writes itself starts with CodejobCommitPrefix, so a reviewer can list the
// agent's commits apart from codejob's own.
func TestCodejob_AllOwnCommitsUsePrefix(t *testing.T) {
	t.Setenv("JULES_API_KEY", "dummy")
	t.Setenv("GH_TOKEN", "dummy")

	var messages []string
	run := func(frontmatter, phase string, runner *mockRunner, job *devflow.CodeJob) {
		t.Helper()
		planIn(t, frontmatter)
		job.SetRunner(runner)
		if err := job.RunCI(phase); err != nil {
			t.Fatalf("%s with %q: %v", phase, frontmatter, err)
		}
		got := commitMessages(runner.calls)
		if len(got) == 0 {
			t.Fatalf("%s with %q: no commit recorded", phase, frontmatter)
		}
		messages = append(messages, got...)
	}

	var published string
	dispatch := devflow.NewCodeJob(&mockSessionDriver{name: "jules", sessionID: "S1"})
	dispatch.SetPublisher(&MockPublisher{PublishFn: func(message, _ string, _, _, _, _, _, _ bool) (gitmod.PushResult, error) {
		published = message
		return gitmod.PushResult{}, nil
	}})
	run("", "dispatch", &mockRunner{}, dispatch)

	run("STATUS: running\nSESSION: S1\nREVIEWER: none\n", "review", &mockRunner{}, devflow.NewCodeJob())
	run("STATUS: running\nSESSION: S1\nREVIEWER: some-agent\n", "review", &mockRunner{}, devflow.NewCodeJob())
	run("STATUS: reviewing\nPR: https://github.com/o/r/pull/1\n", "verdict", &mockRunner{result: `[{"state":"APPROVED"}]`}, devflow.NewCodeJob())
	run("STATUS: reviewing\nPR: https://github.com/o/r/pull/1\n", "verdict", &mockRunner{result: `[{"state":"CHANGES_REQUESTED"}]`}, devflow.NewCodeJob())
	run("STATUS: reviewing\nROUND: 3\nPR: https://github.com/o/r/pull/1\n", "verdict", &mockRunner{result: `[{"state":"CHANGES_REQUESTED"}]`}, devflow.NewCodeJob())

	if !strings.HasPrefix(published, devflow.CodejobCommitPrefix) {
		t.Errorf("sync before dispatch: %q does not start with %q", published, devflow.CodejobCommitPrefix)
	}
	for _, m := range messages {
		if !strings.HasPrefix(m, devflow.CodejobCommitPrefix) {
			t.Errorf("commit %q does not start with %q", m, devflow.CodejobCommitPrefix)
		}
	}
}
