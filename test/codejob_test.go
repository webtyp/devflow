package devflow_test

import (
	"fmt"
	gitmod "webtyp.com/git"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

type mockDriver struct {
	name   string
	result string
	err    error
}

func (m *mockDriver) Name() string                     { return m.name }
func (m *mockDriver) SetLog(_ func(...any))            {}
func (m *mockDriver) Send(_, _ string) (string, error) { return m.result, m.err }

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "codejob-*.md")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestCodeJob_Run_NoArgs_Dispatch(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()
	os.MkdirAll("docs", 0755)
	os.WriteFile("docs/PLAN.md", []byte("---\nPLAN: test\n---\nsome plan"), 0644)

	os.Setenv("JULES_API_KEY", "dummy_key")
	os.Setenv("GH_TOKEN", "dummy_token")
	defer func() {
		os.Unsetenv("JULES_API_KEY")
		os.Unsetenv("GH_TOKEN")
	}()

	d := &mockDriver{name: "mock", result: "ok"}
	job := devflow.NewCodeJob(d)
	job.SetRunner(&mockRunner{})

	// Mock Publisher to satisfy Send's publish-before-dispatch
	job.SetPublisher(&MockPublisher{})

	got, err := job.Run("", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("expected ok, got %q", got)
	}
}

func TestCodeJob_Run_WithMessage_CloseLoop(t *testing.T) {
	// Logic moved to codejob_state_test.go because it requires complex mocking of 'gh' and 'git' commands
}

func TestCodeJob_MessageWithoutPR(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()
	_ = os.MkdirAll("docs", 0755)
	_ = os.WriteFile("docs/PLAN.md", []byte("---\nPLAN: \"test plan\"\nSTATUS: review\n---\n"), 0644)

	job := devflow.NewCodeJob()
	job.SetRunner(&mockRunner{})
	job.SetPublisher(&MockPublisher{})
	_, err := job.Run("some message", "", false)
	if err == nil {
		t.Fatal("expected error when no PR found")
	}
	if !strings.Contains(err.Error(), "no pending PR found") {
		t.Errorf("expected error message about missing PR, got: %v", err)
	}
}

func TestCodeJob_Send_PublishesBeforeDispatch(t *testing.T) {
	path := writeTempFile(t, "---\nPLAN: test\n---\nsome plan")
	published := false
	mockPub := &MockPublisher{
		PublishFn: func(m, tag string, st, sr, sd, sb, stag, sv bool) (gitmod.PushResult, error) {
			published = true
			if !stag || !sd || !sb {
				t.Errorf("expected skipTag, skipDependents, skipBackup to be true")
			}
			if !sv {
				t.Errorf("expected skipVerify to be true for codejob dispatch")
			}
			return gitmod.PushResult{}, nil
		},
	}
	d := &mockDriver{name: "mock", result: "ok"}
	job := devflow.NewCodeJob(d)
	job.SetRunner(&mockRunner{})
	job.SetPublisher(mockPub)

	_, err := job.Send(path)
	if err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Error("Publish should have been called before Send")
	}
}

func TestCodeJob_Send_PublishSilently(t *testing.T) {
	// Verify that Publish is called silently (no logging of summary)
	path := writeTempFile(t, "---\nPLAN: test\n---\nsome plan")
	publishCalled := false
	mockPub := &MockPublisher{
		PublishFn: func(m, tag string, st, sr, sd, sb, stag, sv bool) (gitmod.PushResult, error) {
			publishCalled = true
			return gitmod.PushResult{Summary: "Pushed ✅ v1.2.3"}, nil
		},
	}
	var logged []string
	d := &mockDriver{name: "mock", result: "ok"}
	job := devflow.NewCodeJob(d)
	job.SetRunner(&mockRunner{})
	job.SetPublisher(mockPub)
	job.SetLog(func(args ...any) { logged = append(logged, fmt.Sprint(args...)) })

	_, err := job.Send(path)
	if err != nil {
		t.Fatal(err)
	}
	if !publishCalled {
		t.Error("Publish should have been called")
	}
	if len(logged) > 0 {
		t.Errorf("expected no logging of publish summary, but got: %v", logged)
	}
}

func TestCodeJob_ObjectsToPublish(t *testing.T) {
	tmp := t.TempDir()
	ctx := gitmod.PublishContext{RepoDir: tmp}
	cj := devflow.CodeJob{}

	// nothing -> ActionNone
	action, reason := cj.ObjectsToPublish(ctx)
	if action != gitmod.ActionNone {
		t.Errorf("expected ActionNone, got %v (%s)", action, reason)
	}

	// PLAN.md with STATUS: running -> ActionSkip
	planDir := filepath.Join(tmp, "docs")
	_ = os.MkdirAll(planDir, 0755)
	_ = os.WriteFile(filepath.Join(planDir, "PLAN.md"), []byte("---\nPLAN: test\nSTATUS: running\n---\n"), 0644)
	action, reason = cj.ObjectsToPublish(ctx)
	if action != gitmod.ActionSkip {
		t.Errorf("expected ActionSkip, got %v (%s)", action, reason)
	}
	if reason != gitmod.ObjectionCodejobSession {
		t.Errorf("expected %q, got %q", gitmod.ObjectionCodejobSession, reason)
	}

	// PLAN.md with STATUS: dispatch -> ActionDepsOnly
	_ = os.WriteFile(filepath.Join(planDir, "PLAN.md"), []byte("---\nPLAN: test\nSTATUS: dispatch\n---\n"), 0644)
	action, reason = cj.ObjectsToPublish(ctx)
	if action != gitmod.ActionDepsOnly {
		t.Errorf("expected ActionDepsOnly, got %v (%s)", action, reason)
	}
	if reason != gitmod.ObjectionPlanPending {
		t.Errorf("expected %q, got %q", gitmod.ObjectionPlanPending, reason)
	}
}

// TestCodejobObjector_SkipsOnRunningAndReview locks in the deliberate asymmetry
// with Go.Push: the dependent-cascade objector must skip in BOTH phases, since
// during review the local tree sits on the agent's PR branch and a deps commit
// would land inside that PR. Go.Push, by contrast, only blocks on running.
func TestCodejobObjector_SkipsOnRunningAndReview(t *testing.T) {
	tmp := t.TempDir()
	ctx := gitmod.PublishContext{RepoDir: tmp}
	cj := devflow.CodeJob{}

	planDir := filepath.Join(tmp, "docs")
	_ = os.MkdirAll(planDir, 0755)

	os.WriteFile(filepath.Join(planDir, "PLAN.md"), []byte("---\nPLAN: test\nSTATUS: running\n---\n"), 0644)
	action, reason := cj.ObjectsToPublish(ctx)
	if action != gitmod.ActionSkip {
		t.Errorf("running phase: expected ActionSkip, got %v (%s)", action, reason)
	}
	if reason != gitmod.ObjectionCodejobSession {
		t.Errorf("running phase: expected %q, got %q", gitmod.ObjectionCodejobSession, reason)
	}

	os.WriteFile(filepath.Join(planDir, "PLAN.md"), []byte("---\nPLAN: test\nSTATUS: review\n---\n"), 0644)
	action, reason = cj.ObjectsToPublish(ctx)
	if action != gitmod.ActionSkip {
		t.Errorf("review phase: expected ActionSkip, got %v (%s)", action, reason)
	}
	if reason != gitmod.ObjectionCodejobSession {
		t.Errorf("review phase: expected %q, got %q", gitmod.ObjectionCodejobSession, reason)
	}
}

// TestCodejobObjector_NoObjectionWhenNoState confirms the objector stays silent
// when there is no CODEJOB state and no pending plan.
func TestCodejobObjector_NoObjectionWhenNoState(t *testing.T) {
	tmp := t.TempDir()
	ctx := gitmod.PublishContext{RepoDir: tmp}
	cj := devflow.CodeJob{}

	action, reason := cj.ObjectsToPublish(ctx)
	if action != gitmod.ActionNone {
		t.Errorf("expected ActionNone, got %v (%s)", action, reason)
	}
}
