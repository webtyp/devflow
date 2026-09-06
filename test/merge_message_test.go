package devflow_test

import (
	"webtyp.com/devflow"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePublishMessage(t *testing.T) {
	tests := []struct {
		name       string
		cliMessage string
		cliTag     string
		meta       devflow.PlanMeta
		wantMsg    string
		wantTag    string
		wantErr    error
	}{
		{
			name:       "CLI wins message",
			cliMessage: "cli message",
			meta:       devflow.PlanMeta{Message: "frontmatter message"},
			wantMsg:    "cli message",
		},
		{
			name:       "CLI empty uses frontmatter message",
			cliMessage: "",
			meta:       devflow.PlanMeta{Message: "frontmatter message"},
			wantMsg:    "frontmatter message",
		},
		{
			name:       "both empty message error",
			cliMessage: "",
			meta:       devflow.PlanMeta{Message: ""},
			wantErr:    devflow.ErrNoCloseLoopMessage,
		},
		{
			name:    "CLI wins tag",
			cliTag:  "v2.0.0",
			meta:    devflow.PlanMeta{Message: "ok", Tag: "v1.0.0"},
			wantMsg: "ok",
			wantTag: "v2.0.0",
		},
		{
			name:    "CLI empty uses frontmatter tag",
			cliTag:  "",
			meta:    devflow.PlanMeta{Message: "ok", Tag: "v1.0.0"},
			wantMsg: "ok",
			wantTag: "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, tag, err := devflow.ResolvePublishMessage(tt.cliMessage, tt.cliTag, tt.meta)
			if tt.wantErr != nil {
				if err == nil || err.Error() != tt.wantErr.Error() {
					t.Errorf("ResolvePublishMessage() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePublishMessage() unexpected error: %v", err)
			}
			if msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", msg, tt.wantMsg)
			}
			if tag != tt.wantTag {
				t.Errorf("tag = %q, want %q", tag, tt.wantTag)
			}
		})
	}
}

func TestReadPlanMeta_CheckPlan(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "docs"), 0755)
	path := filepath.Join(tmp, "docs/PLAN.md")
	content := "---\nPLAN: check plan\n---\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	meta, err := devflow.ReadPlanMeta(path)
	if err != nil {
		t.Fatalf("ReadPlanMeta failed: %v", err)
	}
	if meta.Message != "check plan" {
		t.Errorf("expected 'check plan', got %q", meta.Message)
	}
}
