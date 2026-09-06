package devflow_test

import (
	"os"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

func TestDotEnv(t *testing.T) {
	path := "test.env"
	defer os.Remove(path)

	env := devflow.NewDotEnv(path)

	// Test Set and Get
	err := env.Set("FOO", "bar")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, ok := env.Get("FOO")
	if !ok {
		t.Fatal("Get failed to find FOO")
	}
	if val != "bar" {
		t.Errorf("expected bar, got %q", val)
	}

	// Test Update
	err = env.Set("FOO", "baz")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	val, ok = env.Get("FOO")
	if !ok || val != "baz" {
		t.Errorf("expected baz, got %q", val)
	}

	// Test Preservation of Comments and Other Keys
	content := "# This is a comment\n\nNAME=Cesar\n# Another comment\n"
	err = os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = env.Set("NAME", "Antigravity")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)

	if !strings.Contains(s, "# This is a comment") {
		t.Error("lost first comment")
	}
	if !strings.Contains(s, "# Another comment") {
		t.Error("lost second comment")
	}
	if !strings.Contains(s, "NAME=Antigravity") {
		t.Error("failed to update NAME")
	}

	// Test Delete
	err = env.Delete("NAME")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	val, ok = env.Get("NAME")
	if ok {
		t.Error("NAME should have been deleted")
	}

	data, _ = os.ReadFile(path)
	s = string(data)
	if !strings.Contains(s, "# This is a comment") {
		t.Error("comment lost after delete")
	}
	if strings.Contains(s, "NAME=") {
		t.Error("NAME= line still present after delete")
	}

	// Test Append
	err = env.Set("NEW", "val")
	if err != nil {
		t.Fatal(err)
	}
	val, ok = env.Get("NEW")
	if !ok || val != "val" {
		t.Error("failed to append NEW")
	}
}
