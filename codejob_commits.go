package devflow

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
