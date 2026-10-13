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

// sha pads a short sha to 40 characters, so Short (7 chars) stays readable in assertions.
func sha(short string) string { return short + strings.Repeat("0", 40-len(short)) }

type fakeCommit struct {
	short, author, subject string
	numstat, deleted       string
}

// fakePR answers the gh/git commands PRCommitReport runs. fail names a step that errors:
// "view", "fetch", "log", "show" or "diff".
type fakePR struct {
	commits []fakeCommit
	net     string
	fail    string
}

const (
	prURL10    = "https://github.com/o/r/pull/10"
	julesBot   = "google-labs-jules[bot]"
	machineSub = devflow.CodejobCommitPrefix + "status transition to review [pr " + prURL10 + "]"
)

func (p *fakePR) runner() *fakeRunner {
	errStep := func(step string) error {
		if p.fail == step {
			return os.ErrPermission
		}
		return nil
	}
	return &fakeRunner{respond: func(args []string) (string, error) {
		switch {
		case args[0] == "pr" && args[1] == "view":
			return "main feat-x\n", errStep("view")
		case args[0] == "fetch":
			return "", errStep("fetch")
		case args[0] == "log":
			var lines []string
			for _, c := range p.commits {
				lines = append(lines, sha(c.short)+"\t"+c.author+"\t"+c.subject)
			}
			return strings.Join(lines, "\n") + "\n", errStep("log")
		case args[0] == "show":
			target := args[len(args)-1]
			for _, c := range p.commits {
				if sha(c.short) == target {
					if strings.Contains(strings.Join(args, " "), "--diff-filter=D") {
						return c.deleted, errStep("show")
					}
					return c.numstat, errStep("show")
				}
			}
			return "", os.ErrNotExist
		case args[0] == "diff":
			return p.net, errStep("diff")
		}
		return "", nil
	}}
}

// pr10 is the shape of veltylabs/item_catalog PR #10: two agent commits, each followed by
// codejob's status commit; the second agent commit deletes a test file.
func pr10() *fakePR {
	return &fakePR{
		commits: []fakeCommit{
			{short: "a9013a7", author: julesBot, subject: "feat: ServiceCard", numstat: "100\t1\ta.go\n50\t0\tb.go\n40\t0\tc.go\n"},
			{short: "74f0aaf", author: "cdvelop", subject: machineSub, numstat: "1\t1\tdocs/PLAN.md\n"},
			{short: "034c3ea", author: julesBot, subject: "feat: ServiceCard", numstat: "20\t10\ta.go\n19\t9\tb.go\n", deleted: "tests/old_test.go\n"},
			{short: "561d943", author: "cdvelop", subject: machineSub, numstat: "1\t1\tdocs/PLAN.md\n"},
		},
		net: "150\t1\ta.go\n50\t0\tb.go\n21\t0\tc.go\n0\t0\td.go\n",
	}
}

func report(t *testing.T, p *fakePR, reviewed string) devflow.CommitReport {
	t.Helper()
	r, err := devflow.PRCommitReport(p.runner(), prURL10, reviewed)
	if err != nil {
		t.Fatalf("PRCommitReport: %v", err)
	}
	return r
}

func wantReportError(t *testing.T, step, text string) {
	t.Helper()
	p := pr10()
	p.fail = step
	_, err := devflow.PRCommitReport(p.runner(), prURL10, "")
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Fatalf("%s failing: err = %v, want it to contain %q", step, err, text)
	}
}

func TestPRCommitReport_PRViewError(t *testing.T) { wantReportError(t, "view", "could not read PR") }
func TestPRCommitReport_FetchError(t *testing.T)  { wantReportError(t, "fetch", "git fetch failed") }
func TestPRCommitReport_LogError(t *testing.T) {
	wantReportError(t, "log", "could not list PR commits")
}
func TestPRCommitReport_ShowError(t *testing.T) { wantReportError(t, "show", "could not read commit") }

func TestPRCommitReport_SeparatesCodejobCommits(t *testing.T) {
	r := report(t, pr10(), "")
	if r.Base != "main" || r.Head != "feat-x" || len(r.Commits) != 4 {
		t.Fatalf("report = %+v", r)
	}
	for i, want := range []bool{false, true, false, true} {
		if r.Commits[i].Machine != want {
			t.Errorf("commit %d (%s): Machine = %v, want %v", i, r.Commits[i].Short, r.Commits[i].Machine, want)
		}
	}
	if r.LastAgent != sha("034c3ea") {
		t.Errorf("LastAgent = %q, want the second agent commit", r.LastAgent)
	}
	if c := r.Commits[0]; c.Short != "a9013a7" || c.Author != julesBot || c.Files != 3 || c.Added != 190 || c.Removed != 1 {
		t.Errorf("first commit = %+v", c)
	}
}

func TestPRCommitReport_AllAgentCommitsNewWhenNothingReviewed(t *testing.T) {
	r := report(t, pr10(), "")
	for i, want := range []bool{true, false, true, false} {
		if r.Commits[i].New != want {
			t.Errorf("commit %d: New = %v, want %v", i, r.Commits[i].New, want)
		}
	}
}

func TestPRCommitReport_OnlyLaterCommitsNew(t *testing.T) {
	r := report(t, pr10(), sha("a9013a7"))
	for i, want := range []bool{false, false, true, false} {
		if r.Commits[i].New != want {
			t.Errorf("commit %d: New = %v, want %v", i, r.Commits[i].New, want)
		}
	}
}

func TestPRCommitReport_UnknownReviewedCommitMarksAllNew(t *testing.T) {
	r := report(t, pr10(), sha("deadbee"))
	if !r.Commits[0].New || !r.Commits[2].New {
		t.Errorf("a reviewed sha that is not in the PR must mark every agent commit new: %+v", r.Commits)
	}
}

func TestPRCommitReport_WarnsOnDeletions(t *testing.T) {
	r := report(t, pr10(), "")
	if got := r.Commits[2].Deleted; len(got) != 1 || got[0] != "tests/old_test.go" {
		t.Fatalf("Deleted = %v", got)
	}
	if len(r.Commits[0].Deleted) != 0 {
		t.Errorf("first commit deletes nothing, got %v", r.Commits[0].Deleted)
	}
	out := r.String()
	if !strings.Contains(out, "⚠ 034c3ea deletes: tests/old_test.go") || strings.Contains(out, "⚠ a9013a7 deletes") {
		t.Errorf("deletion warnings wrong:\n%s", out)
	}
}

// layout #42: the second commit removed everything the first added; the net diff is empty.
func TestPRCommitReport_WarnsWhenLaterCommitsUndoLines(t *testing.T) {
	p := &fakePR{
		commits: []fakeCommit{
			{short: "1111111", author: julesBot, subject: "feat: layout", numstat: "120\t0\tx.go\n"},
			{short: "2222222", author: julesBot, subject: "chore: cleanup", numstat: "0\t120\tx.go\n", deleted: "x.go\n"},
		},
		net: "",
	}
	r := report(t, p, "")
	if r.UndoneLines != 120 || r.NetAdded != 0 || r.NetFiles != 0 {
		t.Fatalf("UndoneLines = %d, net = %d files +%d", r.UndoneLines, r.NetFiles, r.NetAdded)
	}
	if !strings.Contains(r.String(), "later commits removed 120 lines that earlier commits of this PR added") {
		t.Errorf("missing undone warning:\n%s", r.String())
	}

	clean := report(t, &fakePR{
		commits: []fakeCommit{{short: "3333333", author: julesBot, subject: "feat: y", numstat: "5\t0\ty.go\n"}},
		net:     "5\t0\ty.go\n",
	}, "")
	if clean.UndoneLines != 0 || strings.Contains(clean.String(), "later commits removed") {
		t.Errorf("no lines undone, yet: %d\n%s", clean.UndoneLines, clean.String())
	}
}

func TestPRCommitReport_BinaryFilesCountZeroLines(t *testing.T) {
	r := report(t, &fakePR{
		commits: []fakeCommit{{short: "4444444", author: julesBot, subject: "feat: icon", numstat: "-\t-\timg.png\n3\t1\ta.go\n"}},
		net:     "-\t-\timg.png\n3\t1\ta.go\n",
	}, "")
	if c := r.Commits[0]; c.Files != 2 || c.Added != 3 || c.Removed != 1 {
		t.Errorf("commit = %+v, want 2 files +3 -1", c)
	}
	if r.NetFiles != 2 || r.NetAdded != 3 || r.NetRemoved != 1 {
		t.Errorf("net = %d files +%d -%d", r.NetFiles, r.NetAdded, r.NetRemoved)
	}
}

func TestPRCommitReport_OnlyMachineCommits(t *testing.T) {
	r := report(t, &fakePR{commits: []fakeCommit{{short: "5555555", author: "cdvelop", subject: machineSub, numstat: "1\t1\tdocs/PLAN.md\n"}}}, "")
	if r.LastAgent != "" || r.Commits[0].New {
		t.Errorf("LastAgent = %q, New = %v; want no agent commit", r.LastAgent, r.Commits[0].New)
	}
}

func TestCommitReport_StringShape(t *testing.T) {
	want := strings.Join([]string{
		"Commits on feat-x (2 agent, 2 codejob) — review each one, not only the last:",
		"  🆕 a9013a7  feat: ServiceCard   google-labs-jules[bot]   3 files +190 −1",
		"     74f0aaf  (codejob) status transition to review [pr " + prURL10 + "]",
		"  🆕 034c3ea  feat: ServiceCard   google-labs-jules[bot]   2 files +39 −19",
		"     561d943  (codejob) status transition to review [pr " + prURL10 + "]",
		"     ⚠ 034c3ea deletes: tests/old_test.go",
		"     ⚠ later commits removed 8 lines that earlier commits of this PR added",
		"Net vs main: 4 files +221 −1",
		"Inspect one: git show <sha>",
	}, "\n")
	if got := report(t, pr10(), "").String(); got != want {
		t.Errorf("String():\n%s\n\nwant:\n%s", got, want)
	}

	// A commit already reviewed is listed without the 🆕 marker.
	got := report(t, pr10(), sha("a9013a7")).String()
	if !strings.Contains(got, "\n     a9013a7  feat: ServiceCard") {
		t.Errorf("reviewed commit should be unmarked:\n%s", got)
	}
}
