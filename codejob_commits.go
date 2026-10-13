package devflow

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	gitmod "webtyp.com/git"
)

// CodejobCommitPrefix starts every commit codejob writes itself (status
// transitions, sync, review mark). A reviewer lists them apart from the agent's.
const CodejobCommitPrefix = "chore(codejob): "

// Messages of the commits codejob writes itself; all start with CodejobCommitPrefix.
const (
	commitMsgSync         = CodejobCommitPrefix + "sync before codejob dispatch"
	commitMsgRunning      = CodejobCommitPrefix + "status transition to running [session %s]"
	commitMsgPRReady      = CodejobCommitPrefix + "status transition to %s [pr %s]"
	commitMsgNoReviewer   = CodejobCommitPrefix + "no reviewer set, status to review"
	commitMsgReviewing    = CodejobCommitPrefix + "status transition to reviewing"
	commitMsgApproved     = CodejobCommitPrefix + "reviewer approved, status to review"
	commitMsgRoundCap     = CodejobCommitPrefix + "round cap exceeded, handing over to human"
	commitMsgRedispatch   = CodejobCommitPrefix + "changes requested, re-dispatching to corrector [round %d]"
	commitMsgReviewedUpTo = CodejobCommitPrefix + "reviewed up to %s"
)

// PRCommit is one commit of a PR, oldest first.
type PRCommit struct {
	SHA     string // full sha
	Short   string // 7 chars
	Author  string
	Subject string
	Machine bool // subject starts with CodejobCommitPrefix
	New     bool // agent commit after the reviewed commit (all agent commits when none was reviewed)
	Files   int  // files touched
	Added   int
	Removed int
	Deleted []string // files this commit deletes
}

// CommitReport is what a reviewer must see before judging a PR.
type CommitReport struct {
	Base, Head  string // branch names
	Commits     []PRCommit
	LastAgent   string // sha of the newest non-machine commit, "" if none
	NetFiles    int
	NetAdded    int
	NetRemoved  int
	UndoneLines int // sum of agent Added minus NetAdded, when positive
}

// Errors of PRCommitReport.
const (
	errReadPR     = "codejob: could not read PR %s: %w"
	errFetch      = "codejob: git fetch failed: %w"
	errListCommit = "codejob: could not list PR commits: %w"
	errReadCommit = "codejob: could not read commit %s: %w"
	errPRBranches = "unexpected answer %q"
)

// Markers and lines of CommitReport.String.
const (
	markNew        = "🆕"
	markWarn       = "⚠"
	markMachine    = "(codejob)"
	lineNew        = "  " + markNew + " "
	linePlain      = "     "
	reportHeader   = "Commits on %s (%d agent, %d codejob) — review each one, not only the last:"
	reportAgent    = "%s%s  %s   %s   %s"
	reportMachine  = linePlain + "%s  " + markMachine + " %s"
	reportDeletes  = linePlain + markWarn + " %s deletes: %s"
	reportUndone   = linePlain + markWarn + " later commits removed %d lines that earlier commits of this PR added"
	reportNet      = "Net vs %s: %s"
	reportInspect  = "Inspect one: git show <sha>"
	reportStats    = "%d files +%d −%d"
	shortSHALength = 7
)

// PRCommitReport lists the commits of the PR at prURL, oldest first, relative to reviewedCommit
// (the last agent commit a reviewer was shown; "" = none).
func PRCommitReport(runner gitmod.Runner, prURL, reviewedCommit string) (CommitReport, error) {
	out, err := runner.Run("gh", "pr", "view", prURL, "--json", "baseRefName,headRefName", "--jq", `.baseRefName+" "+.headRefName`)
	if err != nil {
		return CommitReport{}, fmt.Errorf(errReadPR, prURL, err)
	}
	branches := strings.Fields(out)
	if len(branches) != 2 {
		return CommitReport{}, fmt.Errorf(errReadPR, prURL, fmt.Errorf(errPRBranches, out))
	}
	r := CommitReport{Base: branches[0], Head: branches[1]}
	if _, err := runner.Run("git", "fetch", "origin", r.Base, r.Head); err != nil {
		return CommitReport{}, fmt.Errorf(errFetch, err)
	}
	log, err := runner.Run("git", "log", "--reverse", "--format=%H%x09%an%x09%s", "origin/"+r.Base+"..origin/"+r.Head)
	if err != nil {
		return CommitReport{}, fmt.Errorf(errListCommit, err)
	}

	for _, line := range nonEmptyLines(log) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		c := PRCommit{SHA: parts[0], Short: shortSHA(parts[0]), Author: parts[1], Subject: parts[2]}
		c.Machine = strings.HasPrefix(c.Subject, CodejobCommitPrefix)
		stats, err := runner.Run("git", "show", "--format=", "--numstat", c.SHA)
		if err != nil {
			return CommitReport{}, fmt.Errorf(errReadCommit, c.Short, err)
		}
		c.Files, c.Added, c.Removed = numstat(stats)
		deleted, err := runner.Run("git", "show", "--format=", "--diff-filter=D", "--name-only", c.SHA)
		if err != nil {
			return CommitReport{}, fmt.Errorf(errReadCommit, c.Short, err)
		}
		c.Deleted = nonEmptyLines(deleted)
		r.Commits = append(r.Commits, c)
	}

	reviewedAt := -1
	for i, c := range r.Commits {
		if c.SHA == reviewedCommit {
			reviewedAt = i
		}
	}
	agentAdded := 0
	for i := range r.Commits {
		c := &r.Commits[i]
		if c.Machine {
			continue
		}
		c.New = i > reviewedAt
		r.LastAgent = c.SHA
		agentAdded += c.Added
	}

	net, err := runner.Run("git", "diff", "--numstat", "origin/"+r.Base+"...origin/"+r.Head)
	if err != nil {
		return CommitReport{}, fmt.Errorf(errListCommit, err)
	}
	r.NetFiles, r.NetAdded, r.NetRemoved = numstat(net)
	if undone := agentAdded - r.NetAdded; undone > 0 {
		r.UndoneLines = undone
	}
	return r, nil
}

// numstat sums "added<TAB>removed<TAB>path" lines; a binary file ("-") counts as 0 lines.
func numstat(out string) (files, added, removed int) {
	for _, line := range nonEmptyLines(out) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		files++
		a, _ := strconv.Atoi(parts[0])
		d, _ := strconv.Atoi(parts[1])
		added += a
		removed += d
	}
	return files, added, removed
}

func nonEmptyLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func shortSHA(sha string) string {
	if len(sha) > shortSHALength {
		return sha[:shortSHALength]
	}
	return sha
}

// String renders the report for the terminal, one commit per line, oldest first.
func (r CommitReport) String() string {
	agents, machines := 0, 0
	for _, c := range r.Commits {
		if c.Machine {
			machines++
		} else {
			agents++
		}
	}
	lines := []string{fmt.Sprintf(reportHeader, r.Head, agents, machines)}
	for _, c := range r.Commits {
		if c.Machine {
			lines = append(lines, fmt.Sprintf(reportMachine, c.Short, strings.TrimPrefix(c.Subject, CodejobCommitPrefix)))
			continue
		}
		mark := linePlain
		if c.New {
			mark = lineNew
		}
		lines = append(lines, fmt.Sprintf(reportAgent, mark, c.Short, c.Subject, c.Author, fmt.Sprintf(reportStats, c.Files, c.Added, c.Removed)))
	}
	for _, c := range r.Commits {
		if len(c.Deleted) > 0 {
			lines = append(lines, fmt.Sprintf(reportDeletes, c.Short, strings.Join(c.Deleted, ", ")))
		}
	}
	if r.UndoneLines > 0 {
		lines = append(lines, fmt.Sprintf(reportUndone, r.UndoneLines))
	}
	lines = append(lines,
		fmt.Sprintf(reportNet, r.Base, fmt.Sprintf(reportStats, r.NetFiles, r.NetAdded, r.NetRemoved)),
		reportInspect)
	return strings.Join(lines, "\n")
}

// Pull and Close messages around the commit report.
const (
	msgFastForwarded     = "✅ fast-forwarded %s"
	msgReviewerReviewing = "⏳ reviewer is reviewing the PR..."
	msgReportFailed      = markWarn + " could not list commits: %v"
	errNoPendingPR       = "codejob: no pending PR found in PLAN.md frontmatter"
	errNoReview          = "codejob: no review recorded; run codejob pull and review the commits first"
	errNewAgentCommits   = "codejob: %d new agent commit(s) since your last review; run codejob pull"
)

// reportAndMark appends the commit report to statusLine and records the newest agent commit as
// reviewed (one codejob commit, pushed) when it moved. A report error never hides the status and
// never moves the mark.
func (c *CodeJob) reportAndMark(statusLine string, meta PlanMeta) (string, error) {
	report, err := PRCommitReport(c.runner, meta.PR, meta.ReviewedCommit)
	if err != nil {
		return statusLine + "\n" + fmt.Sprintf(msgReportFailed, err), nil
	}
	out := statusLine + "\n" + report.String()
	if report.LastAgent == "" || report.LastAgent == meta.ReviewedCommit {
		return out, nil
	}
	meta.ReviewedCommit = report.LastAgent
	if err := WritePlanMeta(DefaultIssuePromptPath, meta); err != nil {
		return out, err
	}
	_, _ = c.runner.Run("git", "add", DefaultIssuePromptPath)
	_, _ = c.runner.Run("git", "commit", "-m", fmt.Sprintf(commitMsgReviewedUpTo, shortSHA(report.LastAgent)))
	_, _ = c.runner.Run("git", "push")
	return out, nil
}

// reviewIsCurrent refuses to close a PR whose newest agent commit was never shown to the reviewer.
func (c *CodeJob) reviewIsCurrent(meta PlanMeta) error {
	if meta.PR == "" {
		return errors.New(errNoPendingPR)
	}
	if meta.ReviewedCommit == "" {
		return errors.New(errNoReview)
	}
	report, err := PRCommitReport(c.runner, meta.PR, meta.ReviewedCommit)
	if err != nil {
		return err
	}
	if report.LastAgent == meta.ReviewedCommit {
		return nil
	}
	n := 0
	for _, c := range report.Commits {
		if c.New {
			n++
		}
	}
	return fmt.Errorf(errNewAgentCommits, n)
}
