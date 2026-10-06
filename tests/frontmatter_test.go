package devflow_test

import "webtyp.com/devflow"

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	planKey := devflow.FrontmatterKeyPlan
	tagKey := devflow.FrontmatterKeyTag

	tests := []struct {
		name    string
		content string
		want    devflow.PlanMeta
		wantErr error
	}{
		{
			name:    "valid PLAN and tag",
			content: fmt.Sprintf("---\n%s: \"feat: something\"\n%s: v1.0.0\n---\nBody", planKey, tagKey),
			want:    devflow.PlanMeta{Message: "feat: something", Tag: "v1.0.0"},
		},
		{
			name:    "valid PLAN only",
			content: fmt.Sprintf("---\n%s: just message\n---\n", planKey),
			want:    devflow.PlanMeta{Message: "just message"},
		},
		{
			name:    "missing opening fence",
			content: fmt.Sprintf("%s: no fence\n---\n", planKey),
			wantErr: devflow.ErrFrontmatterMissing,
		},
		{
			name:    "unclosed fence",
			content: fmt.Sprintf("---\n%s: no end\n", planKey),
			wantErr: devflow.ErrFrontmatterUnclosed,
		},
		{
			name:    "missing PLAN",
			content: fmt.Sprintf("---\n%s: v1.0.0\n---\n", tagKey),
			wantErr: devflow.ErrFrontmatterNoPlan,
		},
		{
			name:    "legacy message key rejected",
			content: "---\nmessage: old\n---\n",
			wantErr: devflow.ErrFrontmatterNoPlan,
		},
		{
			name:    "with single quotes",
			content: fmt.Sprintf("---\n%s: 'quoted'\n---\n", planKey),
			want:    devflow.PlanMeta{Message: "quoted"},
		},
		{
			name:    "unknown keys ignored",
			content: fmt.Sprintf("---\n%s: ok\nunknown: value\n---\n", planKey),
			want:    devflow.PlanMeta{Message: "ok"},
		},
		{
			name:    "CRLF support",
			content: fmt.Sprintf("---\r\n%s: crlf\r\n---\r\n", planKey),
			want:    devflow.PlanMeta{Message: "crlf"},
		},
		{
			name:    "blank lines internal",
			content: fmt.Sprintf("---\n\n%s: spaced\n\n---\n", planKey),
			want:    devflow.PlanMeta{Message: "spaced"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := devflow.ParseFrontmatter(tt.content)
			if tt.wantErr != nil {
				if err == nil || err.Error() != tt.wantErr.Error() {
					t.Errorf("ParseFrontmatter() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFrontmatter() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseFrontmatter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadPlanMeta(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "PLAN.md")
	content := fmt.Sprintf("---\n%s: from file\n---\n", devflow.FrontmatterKeyPlan)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := devflow.ReadPlanMeta(path)
	if err != nil {
		t.Fatalf("ReadPlanMeta failed: %v", err)
	}
	if meta.Message != "from file" {
		t.Errorf("expected 'from file', got %q", meta.Message)
	}
}

func TestPlanState_ReadFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "PLAN.md")
	content := `---
PLAN: "feat: implemented loop"
TAG: v0.5.0
EXECUTOR: jules
REVIEWER: bob
CORRECTOR: alice
REVIEW_GUIDE: docs/REVIEW.md
STATUS: reviewing
SESSION: S123
REVIEW_SESSION: R456
ROUND: 2
PR: https://github.com/o/r/pull/1
---
# Body content
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := devflow.ReadPlanMeta(path)
	if err != nil {
		t.Fatalf("ReadPlanMeta failed: %v", err)
	}

	if meta.Message != "feat: implemented loop" {
		t.Errorf("expected Message 'feat: implemented loop', got %q", meta.Message)
	}
	if meta.Tag != "v0.5.0" {
		t.Errorf("expected Tag 'v0.5.0', got %q", meta.Tag)
	}
	if meta.Executor != "jules" {
		t.Errorf("expected Executor 'jules', got %q", meta.Executor)
	}
	if meta.Reviewer != "bob" {
		t.Errorf("expected Reviewer 'bob', got %q", meta.Reviewer)
	}
	if meta.Corrector != "alice" {
		t.Errorf("expected Corrector 'alice', got %q", meta.Corrector)
	}
	if meta.ReviewGuide != "docs/REVIEW.md" {
		t.Errorf("expected ReviewGuide 'docs/REVIEW.md', got %q", meta.ReviewGuide)
	}
	if meta.Status != "reviewing" {
		t.Errorf("expected Status 'reviewing', got %q", meta.Status)
	}
	if meta.Session != "S123" {
		t.Errorf("expected Session 'S123', got %q", meta.Session)
	}
	if meta.ReviewSession != "R456" {
		t.Errorf("expected ReviewSession 'R456', got %q", meta.ReviewSession)
	}
	if meta.Round != 2 {
		t.Errorf("expected Round 2, got %d", meta.Round)
	}
	if meta.PR != "https://github.com/o/r/pull/1" {
		t.Errorf("expected PR, got %q", meta.PR)
	}
}

func TestPlanState_WritePreservesBody(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "PLAN.md")
	content := `---
PLAN: "initial"
---
# Original Body
Some description here.
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	meta := devflow.PlanMeta{
		Message:  "updated plan",
		Status:   "running",
		Executor: "jules",
		Round:    1,
	}

	if err := devflow.WritePlanMeta(path, meta); err != nil {
		t.Fatalf("WritePlanMeta failed: %v", err)
	}

	updatedContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	s := string(updatedContent)
	if !strings.Contains(s, "PLAN: \"updated plan\"") {
		t.Errorf("expected serialized PLAN, got:\n%s", s)
	}
	if !strings.Contains(s, "STATUS: running") {
		t.Errorf("expected serialized STATUS, got:\n%s", s)
	}
	if !strings.Contains(s, "ROUND: 1") {
		t.Errorf("expected serialized ROUND, got:\n%s", s)
	}
	if !strings.Contains(s, "# Original Body\nSome description here.\n") {
		t.Errorf("body was not preserved, got:\n%s", s)
	}
}

func TestPlanState_StatusDerivation(t *testing.T) {
	// Let's verify that when Status is missing in the file, it is parsed as empty
	// (so that the status-derivation logic, usually STATUS == "" -> "dispatch", is possible).
	tmp := t.TempDir()
	path := filepath.Join(tmp, "PLAN.md")
	content := `---
PLAN: "test status"
---
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := devflow.ReadPlanMeta(path)
	if err != nil {
		t.Fatalf("ReadPlanMeta failed: %v", err)
	}
	if meta.Status != "" {
		t.Errorf("expected blank Status when absent, got %q", meta.Status)
	}
}
