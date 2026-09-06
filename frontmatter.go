package devflow

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"webtyp.com/markdown"
)

// PlanMeta holds the parsed frontmatter of a docs/PLAN.md file.
type PlanMeta struct {
	Message       string // required: commit message used when closing the loop
	Tag           string // optional: explicit version tag (e.g. "v0.1.0")
	Executor      string // optional: agent that implements (default "jules")
	Reviewer      string // optional: agent that reviews (default "none")
	Corrector     string // optional: agent that applies review feedback (default: executor)
	ReviewGuide   string // optional: path to extra review criteria
	Status        string // optional: dispatch -> running -> reviewing -> review
	Session       string // optional: executor session id
	ReviewSession string // optional: reviewer session id
	Round         int    // optional: executor<->reviewer round count (capped, default 3)
	PR            string // optional: URL of the PR opened by the executor
}

const (
	FrontmatterKeyPlan          = "PLAN"           // required: commit message used when closing the loop
	FrontmatterKeyTag           = "TAG"            // optional: explicit version tag
	FrontmatterKeyExecutor      = "EXECUTOR"       // optional: executor agent
	FrontmatterKeyReviewer      = "REVIEWER"       // optional: reviewer agent
	FrontmatterKeyCorrector     = "CORRECTOR"      // optional: corrector agent
	FrontmatterKeyReviewGuide   = "REVIEW_GUIDE"   // optional: path to review guidelines
	FrontmatterKeyStatus        = "STATUS"         // optional: orchestrator status
	FrontmatterKeySession       = "SESSION"        // optional: executor session ID
	FrontmatterKeyReviewSession = "REVIEW_SESSION" // optional: reviewer session ID
	FrontmatterKeyRound         = "ROUND"          // optional: round count
	FrontmatterKeyPR            = "PR"             // optional: pull request URL
)

// frontmatterHelp is appended to every frontmatter error so the fix is obvious from the
// terminal alone — nobody should need to open the docs to unblock a dispatch.
const frontmatterHelp = `

docs/PLAN.md must OPEN with a frontmatter block — the very first line is '---':

    ---
    PLAN: "feat: what this plan implements"
    TAG: v0.2.0
    ---

    # Plan — ...

  PLAN  REQUIRED. The commit message used when the loop is closed.
  TAG   optional. Explicit version (e.g. v0.2.0); omitted = auto-bump.`

var (
	ErrFrontmatterMissing  = errors.New("plan frontmatter: file must start with a '---' line" + frontmatterHelp)
	ErrFrontmatterUnclosed = errors.New("plan frontmatter: opening '---' has no matching closing '---'" + frontmatterHelp)
	ErrFrontmatterNoPlan   = errors.New("plan frontmatter: missing required 'PLAN:' field (the old 'message:' key was renamed — rename it in your plan)" + frontmatterHelp)
)

// wrapStructuralErr translates markdown's structural frontmatter errors into
// devflow's own (which carry frontmatterHelp); anything else passes through.
func wrapStructuralErr(err error) error {
	switch {
	case errors.Is(err, markdown.ErrFrontmatterMissing):
		return ErrFrontmatterMissing
	case errors.Is(err, markdown.ErrFrontmatterUnclosed):
		return ErrFrontmatterUnclosed
	default:
		return err
	}
}

// metaFromMap maps generic frontmatter key/values to PlanMeta, enforcing the
// devflow-specific rule that 'PLAN' is required.
func metaFromMap(kv map[string]string) (PlanMeta, error) {
	message := kv[FrontmatterKeyPlan]
	if message == "" {
		return PlanMeta{}, ErrFrontmatterNoPlan
	}
	round, _ := strconv.Atoi(kv[FrontmatterKeyRound])
	return PlanMeta{
		Message:       message,
		Tag:           kv[FrontmatterKeyTag],
		Executor:      kv[FrontmatterKeyExecutor],
		Reviewer:      kv[FrontmatterKeyReviewer],
		Corrector:     kv[FrontmatterKeyCorrector],
		ReviewGuide:   kv[FrontmatterKeyReviewGuide],
		Status:        kv[FrontmatterKeyStatus],
		Session:       kv[FrontmatterKeySession],
		ReviewSession: kv[FrontmatterKeyReviewSession],
		Round:         round,
		PR:            kv[FrontmatterKeyPR],
	}, nil
}

// ParseFrontmatter parses the leading frontmatter block of content and maps it
// to PlanMeta, requiring 'PLAN'. Structural parsing is delegated to
// webtyp/markdown; devflow only owns the "which keys are required" rule.
func ParseFrontmatter(content string) (PlanMeta, error) {
	kv, err := markdown.ParseFrontmatter(content)
	if err != nil {
		return PlanMeta{}, wrapStructuralErr(err)
	}
	return metaFromMap(kv)
}

// ReadPlanMeta reads and validates the frontmatter of a plan file at path.
func ReadPlanMeta(path string) (PlanMeta, error) {
	kv, err := markdown.New(".", "", nil).InputPath(path, os.ReadFile).Frontmatter()
	if err != nil {
		return PlanMeta{}, wrapStructuralErr(err)
	}
	return metaFromMap(kv)
}

var ErrNoCloseLoopMessage = errors.New("no close-loop commit message: pass one on the CLI or add 'PLAN:' to the plan frontmatter")

// ResolvePublishMessage picks the effective close-loop commit message and tag:
// an explicit CLI value wins; otherwise the plan frontmatter is used.
func ResolvePublishMessage(cliMessage, cliTag string, meta PlanMeta) (message, tag string, err error) {
	message = cliMessage
	if message == "" {
		message = meta.Message
	}
	if message == "" {
		return "", "", ErrNoCloseLoopMessage
	}
	tag = cliTag
	if tag == "" {
		tag = meta.Tag
	}
	return message, tag, nil
}

// SplitMarkdown splits a markdown content into frontmatter keys and body.
func SplitMarkdown(content string) (map[string]string, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return nil, normalized, ErrFrontmatterMissing
	}
	closingIdx := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			closingIdx = i
			break
		}
	}
	if closingIdx == -1 {
		return nil, normalized, ErrFrontmatterUnclosed
	}
	kv := make(map[string]string)
	for i := 1; i < closingIdx; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
				val = val[1 : len(val)-1]
			}
			kv[key] = val
		}
	}
	body := strings.Join(lines[closingIdx+1:], "\n")
	return kv, body, nil
}

// SerializeFrontmatter serializes PlanMeta back to a YAML-like frontmatter string block.
func SerializeFrontmatter(meta PlanMeta) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	if meta.Message != "" {
		sb.WriteString(fmt.Sprintf("%s: %q\n", FrontmatterKeyPlan, meta.Message))
	}
	if meta.Tag != "" {
		sb.WriteString(fmt.Sprintf("%s: %s\n", FrontmatterKeyTag, meta.Tag))
	}
	if meta.Executor != "" {
		sb.WriteString(fmt.Sprintf("%s: %s\n", FrontmatterKeyExecutor, meta.Executor))
	}
	if meta.Reviewer != "" {
		sb.WriteString(fmt.Sprintf("%s: %s\n", FrontmatterKeyReviewer, meta.Reviewer))
	}
	if meta.Corrector != "" {
		sb.WriteString(fmt.Sprintf("%s: %s\n", FrontmatterKeyCorrector, meta.Corrector))
	}
	if meta.ReviewGuide != "" {
		sb.WriteString(fmt.Sprintf("%s: %s\n", FrontmatterKeyReviewGuide, meta.ReviewGuide))
	}
	if meta.Status != "" {
		sb.WriteString(fmt.Sprintf("%s: %s\n", FrontmatterKeyStatus, meta.Status))
	}
	if meta.Session != "" {
		sb.WriteString(fmt.Sprintf("%s: %s\n", FrontmatterKeySession, meta.Session))
	}
	if meta.ReviewSession != "" {
		sb.WriteString(fmt.Sprintf("%s: %s\n", FrontmatterKeyReviewSession, meta.ReviewSession))
	}
	if meta.Round > 0 {
		sb.WriteString(fmt.Sprintf("%s: %d\n", FrontmatterKeyRound, meta.Round))
	}
	if meta.PR != "" {
		sb.WriteString(fmt.Sprintf("%s: %s\n", FrontmatterKeyPR, meta.PR))
	}
	sb.WriteString("---\n")
	return sb.String()
}

// WritePlanMeta updates or creates a PLAN.md file at path with the given meta, preserving the markdown body.
func WritePlanMeta(path string, meta PlanMeta) error {
	var body string
	content, err := os.ReadFile(path)
	if err == nil {
		_, b, err := SplitMarkdown(string(content))
		if err == nil {
			body = b
		} else {
			body = string(content)
		}
	}

	fm := SerializeFrontmatter(meta)
	newContent := fm + body
	return os.WriteFile(path, []byte(newContent), 0644)
}
