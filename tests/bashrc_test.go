package devflow_test

import "webtyp.com/devflow"

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashrc_SetAndGet(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, ".bashrc")

	b := &devflow.Bashrc{FilePath: tmpFile}

	t.Run("SetNewVariable", func(t *testing.T) {
		err := b.Set("TEST_VAR", "test_value")
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		// Verify file content
		content, _ := os.ReadFile(tmpFile)
		str := string(content)
		if !strings.Contains(str, "# START_DEVFLOW:TEST_VAR") {
			t.Error("Start marker not found")
		}
		if !strings.Contains(str, "# END_DEVFLOW:TEST_VAR") {
			t.Error("End marker not found")
		}
		if !strings.Contains(str, `export TEST_VAR="test_value"`) {
			t.Error("Export statement not found")
		}
	})

	t.Run("GetVariable", func(t *testing.T) {
		value, err := b.Get("TEST_VAR")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "test_value" {
			t.Errorf("Expected 'test_value', got '%s'", value)
		}
	})

	t.Run("UpdateExistingVariable", func(t *testing.T) {
		err := b.Set("TEST_VAR", "new_value")
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		value, err := b.Get("TEST_VAR")
		if err != nil {
			t.Fatalf("Get after update failed: %v", err)
		}
		if value != "new_value" {
			t.Errorf("Expected 'new_value', got '%s'", value)
		}

		// Verify only one section exists
		content, _ := os.ReadFile(tmpFile)
		count := strings.Count(string(content), "# START_DEVFLOW:TEST_VAR")
		if count != 1 {
			t.Errorf("Expected 1 section, found %d", count)
		}
	})

	t.Run("SetWithSpaces", func(t *testing.T) {
		err := b.Set("PATH_VAR", "/path/with spaces/file.txt")
		if err != nil {
			t.Fatalf("Set with spaces failed: %v", err)
		}

		value, err := b.Get("PATH_VAR")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "/path/with spaces/file.txt" {
			t.Errorf("Expected path with spaces, got '%s'", value)
		}
	})

	t.Run("RemoveVariable", func(t *testing.T) {
		// Set then remove
		b.Set("TEMP_VAR", "temporary")
		err := b.Set("TEMP_VAR", "") // Empty value removes
		if err != nil {
			t.Fatalf("Remove failed: %v", err)
		}

		_, err = b.Get("TEMP_VAR")
		if err == nil {
			t.Error("Expected error when getting removed variable")
		}

		content, _ := os.ReadFile(tmpFile)
		if strings.Contains(string(content), "TEMP_VAR") {
			t.Error("Variable not removed from file")
		}
	})

	t.Run("GetNonExistent", func(t *testing.T) {
		_, err := b.Get("NONEXISTENT")
		if err == nil {
			t.Error("Expected error for non-existent variable")
		}
	})
}

func TestBashrc_FileNotExists(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, ".bashrc")

	b := &devflow.Bashrc{FilePath: tmpFile}

	t.Run("SetCreatesFile", func(t *testing.T) {
		err := b.Set("NEW_VAR", "value")
		if err != nil {
			t.Fatalf("Set failed on new file: %v", err)
		}

		value, err := b.Get("NEW_VAR")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "value" {
			t.Errorf("Expected 'value', got '%s'", value)
		}
	})
}

func TestBashrc_MultipleSections(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, ".bashrc")

	b := &devflow.Bashrc{FilePath: tmpFile}

	// Create duplicate sections manually
	content := `# START_DEVFLOW:DUP_VAR
export DUP_VAR="first"
# END_DEVFLOW:DUP_VAR
# START_DEVFLOW:DUP_VAR
export DUP_VAR="second"
# END_DEVFLOW:DUP_VAR`

	os.WriteFile(tmpFile, []byte(content), 0644)

	t.Run("DeduplicateOnUpdate", func(t *testing.T) {
		err := b.Set("DUP_VAR", "fixed")
		if err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		// Should have only one section now
		fileContent, _ := os.ReadFile(tmpFile)
		count := strings.Count(string(fileContent), "# START_DEVFLOW:DUP_VAR")
		if count != 1 {
			t.Errorf("Expected 1 section after dedup, found %d", count)
		}

		value, err := b.Get("DUP_VAR")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if value != "fixed" {
			t.Errorf("Expected 'fixed', got '%s'", value)
		}
	})
}

func TestBashrc_ExtractValue(t *testing.T) {
	b := &devflow.Bashrc{}

	tests := []struct {
		name        string
		exportLine  string
		key         string
		expected    string
		shouldError bool
	}{
		{
			name:       "WithQuotes",
			exportLine: `export MY_VAR="value"`,
			key:        "MY_VAR",
			expected:   "value",
		},
		{
			name:       "WithSpaces",
			exportLine: `export MY_VAR="value with spaces"`,
			key:        "MY_VAR",
			expected:   "value with spaces",
		},
		{
			name:       "WithPath",
			exportLine: `export PATH_VAR="/usr/local/bin"`,
			key:        "PATH_VAR",
			expected:   "/usr/local/bin",
		},
		{
			name:        "InvalidFormat",
			exportLine:  `MY_VAR="value"`,
			key:         "MY_VAR",
			shouldError: true,
		},
		{
			name:        "WrongKey",
			exportLine:  `export OTHER_VAR="value"`,
			key:         "MY_VAR",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := b.ExtractValue(tt.exportLine, tt.key)
			if tt.shouldError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if value != tt.expected {
					t.Errorf("Expected '%s', got '%s'", tt.expected, value)
				}
			}
		})
	}
}

func TestBashrc_NoChangeWhenSame(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, ".bashrc")

	b := &devflow.Bashrc{FilePath: tmpFile}

	// Set initial value
	b.Set("SAME_VAR", "same_value")

	// Get file mod time
	stat1, _ := os.Stat(tmpFile)

	// Set same value again
	b.Set("SAME_VAR", "same_value")

	// Check mod time didn't change
	stat2, _ := os.Stat(tmpFile)

	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Error("File was modified when content was identical")
	}
}

func TestBashrc_PreserveOtherContent(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, ".bashrc")

	// Create file with existing content
	existing := `# User's custom .bashrc
export PATH="$PATH:/custom/path"
alias ll='ls -la'
`
	os.WriteFile(tmpFile, []byte(existing), 0644)

	b := &devflow.Bashrc{FilePath: tmpFile}

	// Add devflow variable
	b.Set("DEV_VAR", "dev_value")

	// Verify original content preserved
	content, _ := os.ReadFile(tmpFile)
	str := string(content)

	if !strings.Contains(str, "# User's custom .bashrc") {
		t.Error("Original comment lost")
	}
	if !strings.Contains(str, `export PATH="$PATH:/custom/path"`) {
		t.Error("Original PATH lost")
	}
	if !strings.Contains(str, "alias ll='ls -la'") {
		t.Error("Original alias lost")
	}
	if !strings.Contains(str, `export DEV_VAR="dev_value"`) {
		t.Error("New variable not added")
	}
}
