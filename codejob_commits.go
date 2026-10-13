package devflow

import gitmod "webtyp.com/git"

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

// PRCommitReport lists the commits of the PR at prURL, relative to reviewedCommit.
func PRCommitReport(runner gitmod.Runner, prURL, reviewedCommit string) (CommitReport, error) {
	return CommitReport{}, nil
}

// String renders the report for the terminal.
func (r CommitReport) String() string { return "" }
