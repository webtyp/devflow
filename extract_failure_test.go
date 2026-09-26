package devflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFirstFailureNamesEveryFailingStage(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "single wasm failure",
			output: "Tests failed: vet ✅, race ✅, tests ✅, wasm ❌ v0.0.56 (24.1s)",
			want:   "wasm",
		},
		{
			name:   "submodule wasm failure",
			output: "Tests failed: vet ✅, race ✅, tests ✅, wasm tests ❌ (3s)",
			want:   "wasm tests",
		},
		{
			name:   "multiple failures",
			output: "Tests failed: vet ❌, race ✅, tests ❌, wasm ✅ (3s)",
			want:   "vet, tests",
		},
		{
			name:   "timeout failure",
			output: "Tests failed: timeout: TestX (exceeded 60s) ❌ (70s)",
			want:   "timeout: TestX (exceeded 60s)",
		},
		{
			name:   "go mod tidy failure fallback",
			output: "go: updates to go.mod needed",
			want:   "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFirstFailure(tt.output)
			if got != tt.want {
				t.Errorf("extractFirstFailure(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

func TestWriteGateLog(t *testing.T) {
	depName := "auth/tests"
	content := "test output log content"

	path := writeGateLog(depName, content)
	if path == "" {
		t.Fatal("expected writeGateLog to return a non-empty path")
	}
	t.Cleanup(func() {
		os.Remove(path)
	})

	expectedFileName := "gopush-gate-auth-tests.log"
	if filepath.Base(path) != expectedFileName {
		t.Errorf("expected file base name %q, got %q", expectedFileName, filepath.Base(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read created gate log file: %v", err)
	}

	if string(data) != content {
		t.Errorf("expected file content %q, got %q", content, string(data))
	}
}
