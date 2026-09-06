package devflow_test

import (
	"fmt"
	gitmod "webtyp.com/git"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/command"
	"webtyp.com/devflow"
)

// memStore is an in-memory gitmod.SecretStore for tests.
type memStore map[string]string

var _ gitmod.SecretStore = (memStore)(nil)

func (m memStore) Set(key, value string) error {
	m[key] = value
	return nil
}

func (m memStore) Get(key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("no value for key %q", key)
	}
	return v, nil
}

func (m memStore) Delete(key string) error {
	delete(m, key)
	return nil
}

type mockRunner struct {
	calls  []string
	result string
	err    error
}

func (m *mockRunner) Run(name string, args ...string) (string, error) {
	full := name + " " + strings.Join(args, " ")
	m.calls = append(m.calls, full)
	if name == "gh" && strings.Contains(full, "reviews") {
		return m.result, m.err
	}
	return "ok", nil
}

func TestAuth_EnvVarThenKeyring(t *testing.T) {
	// Set env vars
	os.Setenv("JULES_API_KEY", "env_jules_key")
	os.Setenv("GH_TOKEN", "env_gh_token")
	defer func() {
		os.Unsetenv("JULES_API_KEY")
		os.Unsetenv("GH_TOKEN")
	}()

	auth, err := devflow.NewJulesAuth()
	if err != nil {
		t.Fatal(err)
	}
	key, err := auth.EnsureAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if key != "env_jules_key" {
		t.Errorf("expected env key, got %q", key)
	}

	patAuth := gitmod.NewGitHubPATAuth(memStore{})
	tok, err := patAuth.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	if tok != "env_gh_token" {
		t.Errorf("expected env token, got %q", tok)
	}
}

type mockSessionDriver struct {
	name      string
	sessionID string
}

func (m *mockSessionDriver) Name() string                     { return m.name }
func (m *mockSessionDriver) SetLog(_ func(...any))            {}
func (m *mockSessionDriver) Send(_, _ string) (string, error) { return "ok", nil }
func (m *mockSessionDriver) SessionID() string                { return m.sessionID }

func TestCI_Dispatch_WritesRunning(t *testing.T) {
	tmp := t.TempDir()
	defer testChdir(t, tmp)()

	_ = os.MkdirAll("docs", 0755)
	planPath := "docs/PLAN.md"
	content := `---
PLAN: "feat: ci test"
---
some plan body
`
	if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	driver := &mockSessionDriver{name: "jules", sessionID: "S-12345"}
	job := devflow.NewCodeJob(driver)
	job.SetPublisher(&MockPublisher{}) // prevent real publishing
	runner := &mockRunner{}
	job.SetRunner(runner)

	os.Setenv("JULES_API_KEY", "dummy")
	os.Setenv("GH_TOKEN", "dummy")
	defer func() {
		os.Unsetenv("JULES_API_KEY")
		os.Unsetenv("GH_TOKEN")
	}()

	err := job.RunCI("dispatch")
	if err != nil {
		t.Fatalf("RunCI dispatch failed: %v", err)
	}

	// Verify PLAN.md has STATUS: running and SESSION: S-12345
	meta, err := devflow.ReadPlanMeta(planPath)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Status != "running" {
		t.Errorf("expected STATUS to be running, got %q", meta.Status)
	}
	if meta.Session != "S-12345" {
		t.Errorf("expected SESSION to be S-12345, got %q", meta.Session)
	}

	// Verify git commit was called
	committed := false
	for _, c := range runner.calls {
		if strings.Contains(c, "git commit -m") && strings.Contains(c, "status transition to running") {
			committed = true
			break
		}
	}
	if !committed {
		t.Error("expected status transition commit to be executed")
	}
}

func TestInitAction_Scaffolding(t *testing.T) {
	tmp := t.TempDir()
	defer testChdir(t, tmp)()

	// Mock gh repo view and gh secret set
	orig := command.Exec
	defer func() { command.Exec = orig }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		full := name + " " + strings.Join(args, " ")
		switch {
		case full == "gh repo view --json owner,name":
			return exec.Command("echo", `{"owner":{"login":"testowner"},"name":"testrepo"}`)
		default:
			return exec.Command("echo", "ok")
		}
	}

	// Set credentials in env to avoid keyring prompt
	os.Setenv("JULES_API_KEY", "dummy_key")
	os.Setenv("GH_TOKEN", "dummy_tok")
	defer func() {
		os.Unsetenv("JULES_API_KEY")
		os.Unsetenv("GH_TOKEN")
	}()

	workflowPath := filepath.Join(".github", "workflows", "codejob.yml")

	// 1. Creates when absent
	err := devflow.InitCodejobAction(false, "", "")
	if err != nil {
		t.Fatalf("InitCodejobAction failed: %v", err)
	}

	if _, err := os.Stat(workflowPath); err != nil {
		t.Error("expected workflow file to be created")
	}

	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "name: CodeJob") {
		t.Error("scaffolded workflow has incorrect content")
	}

	// 2. Idempotent (does not overwrite existing without force)
	dummy := "dummy content"
	_ = os.WriteFile(workflowPath, []byte(dummy), 0644)

	err = devflow.InitCodejobAction(false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	content, _ = os.ReadFile(workflowPath)
	if string(content) != dummy {
		t.Error("expected idempotent execution to preserve existing workflow file")
	}

	// 3. Force overwrites
	err = devflow.InitCodejobAction(true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	content, _ = os.ReadFile(workflowPath)
	if string(content) == dummy {
		t.Error("expected force execution to overwrite existing workflow file")
	}
}

func TestInitAction_SecretScope(t *testing.T) {
	recorded := []string{}
	orig := command.Exec
	defer func() { command.Exec = orig }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		full := name + " " + strings.Join(args, " ")
		recorded = append(recorded, full)
		return exec.Command("echo", "ok")
	}

	gh, err := gitmod.NewGitHub(func(args ...any) {}, nil, gitmod.NewMockGitHubAuth())
	if err != nil {
		t.Fatal(err)
	}

	// Repo-level secret
	err = gh.SetSecretWithScope("o/r", "KEY", "val", "", "")
	if err != nil {
		t.Fatal(err)
	}

	foundRepoSecretCmd := false
	for _, c := range recorded {
		if strings.Contains(c, "gh secret set KEY") && strings.Contains(c, "--repo o/r") {
			foundRepoSecretCmd = true
			break
		}
	}
	if !foundRepoSecretCmd {
		t.Errorf("expected repo secret command, got: %v", recorded)
	}

	// Org-level secret
	recorded = []string{}
	err = gh.SetSecretWithScope("o/r", "KEY", "val", "myorg", "all")
	if err != nil {
		t.Fatal(err)
	}

	foundOrgSecretCmd := false
	for _, c := range recorded {
		if strings.Contains(c, "gh secret set KEY") && strings.Contains(c, "--org myorg") && strings.Contains(c, "--visibility all") {
			foundOrgSecretCmd = true
			break
		}
	}
	if !foundOrgSecretCmd {
		t.Errorf("expected org secret command, got: %v", recorded)
	}
}

func TestActionTemplate_Contract(t *testing.T) {
	args := []string{"codejob", "--ci", "dispatch"}
	opts := devflow.ParseCodeJobFlags(args)
	if opts.CIPhase != "dispatch" {
		t.Errorf("expected CIPhase 'dispatch', got %q", opts.CIPhase)
	}

	args = []string{"codejob", "--ci", "publish", "--force"}
	opts = devflow.ParseCodeJobFlags(args)
	if opts.CIPhase != "publish" {
		t.Errorf("expected CIPhase 'publish', got %q", opts.CIPhase)
	}
	if !opts.Force {
		t.Error("expected Force option to be parsed")
	}
}

type mockPublisher struct {
	published bool
	message   string
	tag       string
}

func (m *mockPublisher) Publish(message, tag string, skipTests, skipRace, skipDependents, skipBackup, skipTag, skipVerify bool) (gitmod.PushResult, error) {
	m.published = true
	m.message = message
	m.tag = tag
	return gitmod.PushResult{Tag: "v0.5.0", Summary: "Mock published v0.5.0"}, nil
}

func TestCI_Publish_TagOnly(t *testing.T) {
	tmp := t.TempDir()
	defer testChdir(t, tmp)()

	_ = os.MkdirAll("docs", 0755)
	planPath := "docs/PLAN.md"
	content := `---
PLAN: "feat: release version"
TAG: v0.5.0
STATUS: review
PR: https://github.com/o/r/pull/1
---
some plan body
`
	if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	job := devflow.NewCodeJob()
	pub := &mockPublisher{}
	job.SetPublisher(pub)

	// Mock git and gh commands needed by MergeAndPublish
	runner := &mockRunner{}
	job.SetRunner(runner)

	err := job.RunCI("publish")
	if err != nil {
		t.Fatalf("RunCI publish failed: %v", err)
	}

	// Verify that PLAN.md was deleted
	if _, err := os.Stat(planPath); err == nil {
		t.Error("expected docs/PLAN.md to be deleted after publish")
	}

	// Verify publisher was called
	if !pub.published {
		t.Error("expected publisher.Publish to be called")
	}
	if pub.message != "feat: release version" {
		t.Errorf("expected publish message 'feat: release version', got %q", pub.message)
	}
	if pub.tag != "v0.5.0" {
		t.Errorf("expected publish tag 'v0.5.0', got %q", pub.tag)
	}
}

func TestCI_Publish_NoopWhenNoPlan(t *testing.T) {
	tmp := t.TempDir()
	defer testChdir(t, tmp)()

	job := devflow.NewCodeJob()
	pub := &mockPublisher{}
	job.SetPublisher(pub)

	err := job.RunCI("publish")
	if err != nil {
		t.Fatalf("expected RunCI publish to be no-op when plan is absent, got error: %v", err)
	}

	if pub.published {
		t.Error("expected no publish action when PLAN.md is absent")
	}
}

func TestCI_Verdict_Approved(t *testing.T) {
	tmp := t.TempDir()
	defer testChdir(t, tmp)()

	_ = os.MkdirAll("docs", 0755)
	planPath := "docs/PLAN.md"
	content := `---
PLAN: "feat: ci test"
STATUS: reviewing
PR: https://github.com/o/r/pull/1
---
some plan body
`
	if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	job := devflow.NewCodeJob()
	runner := &mockRunner{result: `[{"state":"APPROVED"}]`}
	job.SetRunner(runner)

	err := job.RunCI("verdict")
	if err != nil {
		t.Fatalf("RunCI verdict failed: %v", err)
	}

	// Verify PLAN.md has STATUS: review
	meta, err := devflow.ReadPlanMeta(planPath)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Status != "review" {
		t.Errorf("expected STATUS to be review, got %q", meta.Status)
	}

	// Verify git commit was called with approved status
	committed := false
	for _, c := range runner.calls {
		if strings.Contains(c, "git commit -m") && strings.Contains(c, "reviewer approved") {
			committed = true
			break
		}
	}
	if !committed {
		t.Error("expected approved status transition commit to be executed")
	}
}

func TestCI_Verdict_ChangesRequested_RoundInc(t *testing.T) {
	tmp := t.TempDir()
	defer testChdir(t, tmp)()

	_ = os.MkdirAll("docs", 0755)
	planPath := "docs/PLAN.md"
	content := `---
PLAN: "feat: ci test"
STATUS: reviewing
PR: https://github.com/o/r/pull/1
ROUND: 1
---
some plan body
`
	if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	job := devflow.NewCodeJob()
	runner := &mockRunner{result: `[{"state":"CHANGES_REQUESTED"}]`}
	job.SetRunner(runner)

	err := job.RunCI("verdict")
	if err != nil {
		t.Fatalf("RunCI verdict failed: %v", err)
	}

	// Verify PLAN.md has STATUS: running and ROUND: 2
	meta, err := devflow.ReadPlanMeta(planPath)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Status != "running" {
		t.Errorf("expected STATUS to be running, got %q", meta.Status)
	}
	if meta.Round != 2 {
		t.Errorf("expected ROUND to be incremented to 2, got %d", meta.Round)
	}

	// Verify git commit was called with round increment
	committed := false
	for _, c := range runner.calls {
		if strings.Contains(c, "git commit -m") && strings.Contains(c, "re-dispatching to corrector [round 2]") {
			committed = true
			break
		}
	}
	if !committed {
		t.Error("expected changes requested status transition commit to be executed")
	}
}

func TestCI_Verdict_RoundCap(t *testing.T) {
	tmp := t.TempDir()
	defer testChdir(t, tmp)()

	_ = os.MkdirAll("docs", 0755)
	planPath := "docs/PLAN.md"
	content := `---
PLAN: "feat: ci test"
STATUS: reviewing
PR: https://github.com/o/r/pull/1
ROUND: 3
---
some plan body
`
	if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	job := devflow.NewCodeJob()
	runner := &mockRunner{result: `[{"state":"CHANGES_REQUESTED"}]`}
	job.SetRunner(runner)

	err := job.RunCI("verdict")
	if err != nil {
		t.Fatalf("RunCI verdict failed: %v", err)
	}

	// Verify PLAN.md has STATUS: review (hand over to human) and ROUND: 4
	meta, err := devflow.ReadPlanMeta(planPath)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Status != "review" {
		t.Errorf("expected STATUS to be review after cap, got %q", meta.Status)
	}
	if meta.Round != 4 {
		t.Errorf("expected ROUND to be incremented to 4, got %d", meta.Round)
	}

	// Verify git commit was called with round cap handing over message
	committed := false
	for _, c := range runner.calls {
		if strings.Contains(c, "git commit -m") && strings.Contains(c, "round cap exceeded, handing over") {
			committed = true
			break
		}
	}
	if !committed {
		t.Error("expected round cap exceeded status transition commit to be executed")
	}
}

func TestCI_PROpened_DispatchesReviewer(t *testing.T) {
	tmp := t.TempDir()
	defer testChdir(t, tmp)()

	_ = os.MkdirAll("docs", 0755)
	planPath := "docs/PLAN.md"
	content := `---
PLAN: "feat: ci test"
STATUS: running
SESSION: S-12345
REVIEWER: some-agent
---
some plan body
`
	if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	job := devflow.NewCodeJob()
	runner := &mockRunner{}
	job.SetRunner(runner)

	err := job.RunCI("review")
	if err != nil {
		t.Fatalf("RunCI review failed: %v", err)
	}

	// Verify PLAN.md has STATUS: reviewing and REVIEW_SESSION: R-S-12345
	meta, err := devflow.ReadPlanMeta(planPath)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Status != "reviewing" {
		t.Errorf("expected STATUS to be reviewing, got %q", meta.Status)
	}
	if meta.ReviewSession != "R-S-12345" {
		t.Errorf("expected REVIEW_SESSION to be R-S-12345, got %q", meta.ReviewSession)
	}

	// Verify git commit was called
	committed := false
	for _, c := range runner.calls {
		if strings.Contains(c, "git commit -m") && strings.Contains(c, "status transition to reviewing") {
			committed = true
			break
		}
	}
	if !committed {
		t.Error("expected status transition commit to be executed")
	}
}

func TestCI_PROpened_NoReviewer(t *testing.T) {
	tmp := t.TempDir()
	defer testChdir(t, tmp)()

	_ = os.MkdirAll("docs", 0755)
	planPath := "docs/PLAN.md"
	content := `---
PLAN: "feat: ci test"
STATUS: running
SESSION: S-12345
REVIEWER: none
---
some plan body
`
	if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	job := devflow.NewCodeJob()
	runner := &mockRunner{}
	job.SetRunner(runner)

	err := job.RunCI("review")
	if err != nil {
		t.Fatalf("RunCI review failed: %v", err)
	}

	// Verify PLAN.md has STATUS: review and REVIEW_SESSION is empty
	meta, err := devflow.ReadPlanMeta(planPath)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Status != "review" {
		t.Errorf("expected STATUS to be review, got %q", meta.Status)
	}
	if meta.ReviewSession != "" {
		t.Errorf("expected empty REVIEW_SESSION, got %q", meta.ReviewSession)
	}

	// Verify git commit was called
	committed := false
	for _, c := range runner.calls {
		if strings.Contains(c, "git commit -m") && strings.Contains(c, "no reviewer set") {
			committed = true
			break
		}
	}
	if !committed {
		t.Error("expected status transition commit to be executed")
	}
}
