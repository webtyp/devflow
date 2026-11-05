package devflow_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"webtyp.com/devflow"
)

func TestTestsIndex_AbsentIsNotChecked(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "tests"), 0755)
	os.WriteFile(filepath.Join(dir, "tests", "a_test.go"), []byte(`package x_test
import "testing"
func TestA(t *testing.T) {}
`), 0644)

	report, err := devflow.CheckTestsIndex(dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if report.Present != false {
		t.Errorf("expected Present to be false")
	}
	if len(report.Problems) != 0 {
		t.Errorf("expected 0 problems, got %v", len(report.Problems))
	}
}

func TestTestsIndex_InSync(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "tests"), 0755)
	os.WriteFile(filepath.Join(dir, "tests", "a_test.go"), []byte(`package x_test
import "testing"
func TestA(t *testing.T) {}
func TestMain(m *testing.M) {}
func BenchmarkA(b *testing.B) {}
`), 0644)

	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "docs", "TESTS.md"), []byte(`[TestA](../tests/a_test.go)`), 0644)

	report, err := devflow.CheckTestsIndex(dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if report.Present != true {
		t.Errorf("expected Present to be true")
	}
	if len(report.Problems) != 0 {
		t.Errorf("expected 0 problems, got %v", len(report.Problems))
	}
}

func TestTestsIndex_MissingRow(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "tests"), 0755)
	os.WriteFile(filepath.Join(dir, "tests", "a_test.go"), []byte(`package x_test
import "testing"
func TestA(t *testing.T) {}
func TestB(t *testing.T) {}
`), 0644)

	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "docs", "TESTS.md"), []byte(`[TestA](../tests/a_test.go)`), 0644)

	report, err := devflow.CheckTestsIndex(dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	expected := []string{"docs/TESTS.md: missing row for TestB (tests/a_test.go)"}
	if !reflect.DeepEqual(report.Problems, expected) {
		t.Errorf("expected problems %v, got %v", expected, report.Problems)
	}
}

func TestTestsIndex_OrphanRow(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "tests"), 0755)
	os.WriteFile(filepath.Join(dir, "tests", "a_test.go"), []byte(`package x_test
import "testing"
func TestA(t *testing.T) {}
`), 0644)

	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "docs", "TESTS.md"), []byte(`
[TestA](../tests/a_test.go)
[TestGone](../tests/a_test.go)
`), 0644)

	report, err := devflow.CheckTestsIndex(dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	expected := []string{"docs/TESTS.md: row TestGone → tests/a_test.go: test not found"}
	if !reflect.DeepEqual(report.Problems, expected) {
		t.Errorf("expected problems %v, got %v", expected, report.Problems)
	}
}

func TestTestsIndex_WrongFileIsBoth(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "tests"), 0755)
	os.WriteFile(filepath.Join(dir, "tests", "a_test.go"), []byte(`package x_test
import "testing"
func TestA(t *testing.T) {}
`), 0644)

	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "docs", "TESTS.md"), []byte(`[TestA](../tests/b_test.go)`), 0644)

	report, err := devflow.CheckTestsIndex(dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	expected := []string{
		"docs/TESTS.md: missing row for TestA (tests/a_test.go)",
		"docs/TESTS.md: row TestA → tests/b_test.go: test not found",
	}
	if !reflect.DeepEqual(report.Problems, expected) {
		t.Errorf("expected problems %v, got %v", expected, report.Problems)
	}
}

func TestTestsIndex_SkipsTempAndSubmodules(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "tests"), 0755)
	os.WriteFile(filepath.Join(dir, "tests", "a_test.go"), []byte(`package x_test
import "testing"
func TestA(t *testing.T) {}
`), 0644)

	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "docs", "TESTS.md"), []byte(`[TestA](../tests/a_test.go)`), 0644)

	os.MkdirAll(filepath.Join(dir, "_temp"), 0755)
	os.WriteFile(filepath.Join(dir, "_temp", "x_test.go"), []byte(`package x_test
import "testing"
func TestX(t *testing.T) {}
`), 0644)

	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "go.mod"), []byte(`module sub`), 0644)
	os.WriteFile(filepath.Join(dir, "sub", "s_test.go"), []byte(`package s_test
import "testing"
func TestS(t *testing.T) {}
`), 0644)

	report, err := devflow.CheckTestsIndex(dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(report.Problems) != 0 {
		t.Errorf("expected 0 problems, got %v", len(report.Problems))
	}
}

func TestTestsIndex_RootLevelTest(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "root_test.go"), []byte(`package root_test
import "testing"
func TestR(t *testing.T) {}
`), 0644)

	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "docs", "TESTS.md"), []byte(`[TestR](../root_test.go)`), 0644)

	report, err := devflow.CheckTestsIndex(dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(report.Problems) != 0 {
		t.Errorf("expected 0 problems, got %v", len(report.Problems))
	}
}
