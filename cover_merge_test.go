package devflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The defect this file guards against, reproduced from the real case:
//
// webtyp/auth moved its suite into tests/ with its own go.mod. gotest ran that
// submodule and its tests covered 65.7% of the parent, but the reported number
// was 1.2% — the root profile alone. The submodule ran with -cover but no
// -coverprofile, so its result existed only as text in the log and never
// reached the percentage.
//
// The second trap is in the merge itself: a submodule runs with -coverpkg over
// the parent, so it reports the SAME blocks the root run does. Appending the
// profiles instead of combining them feeds `go tool cover` every statement
// twice, which halves the reported percentage.

func writeProfile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMergeCoverProfilesCombinesRepeatedBlocks(t *testing.T) {
	dir := t.TempDir()

	// Same two blocks in both profiles — exactly what -coverpkg over the parent
	// produces. The root run covered neither; the submodule covered both.
	root := writeProfile(t, dir, "root.out", "mode: set\n"+
		"m/a.go:1.1,2.1 1 0\n"+
		"m/b.go:3.1,4.1 2 0\n")
	sub := writeProfile(t, dir, "sub.out", "mode: set\n"+
		"m/a.go:1.1,2.1 1 1\n"+
		"m/b.go:3.1,4.1 2 1\n")

	out := filepath.Join(dir, "merged.out")
	if !mergeCoverProfiles(out, root, sub) {
		t.Fatal("mergeCoverProfiles reported nothing written")
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")

	// One mode header + one line per distinct block. Three would mean the
	// profiles were appended, and every statement would be counted twice.
	if len(lines) != 3 {
		t.Fatalf("merged profile has %d lines, want 3 (mode + 2 blocks); appending duplicates halves the reported coverage:\n%s", len(lines), got)
	}
	if lines[0] != "mode: set" {
		t.Errorf("first line = %q, want %q", lines[0], "mode: set")
	}
	for _, want := range []string{"m/a.go:1.1,2.1 1 1", "m/b.go:3.1,4.1 2 1"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("merged profile lost the submodule's hit; want line %q in:\n%s", want, got)
		}
	}
}

func TestMergeCoverProfilesModeSemantics(t *testing.T) {
	dir := t.TempDir()

	// "set" records presence, so combining two hits stays 1.
	setOut := filepath.Join(dir, "set.out")
	if !mergeCoverProfiles(setOut,
		writeProfile(t, dir, "s1.out", "mode: set\nm/a.go:1.1,2.1 1 1\n"),
		writeProfile(t, dir, "s2.out", "mode: set\nm/a.go:1.1,2.1 1 1\n")) {
		t.Fatal("merge failed for mode set")
	}
	if got, _ := os.ReadFile(setOut); !strings.Contains(string(got), "m/a.go:1.1,2.1 1 1") {
		t.Errorf("mode set must combine as max, got:\n%s", got)
	}

	// "atomic"/"count" record hit counts, so combining sums them.
	atomicOut := filepath.Join(dir, "atomic.out")
	if !mergeCoverProfiles(atomicOut,
		writeProfile(t, dir, "a1.out", "mode: atomic\nm/a.go:1.1,2.1 1 2\n"),
		writeProfile(t, dir, "a2.out", "mode: atomic\nm/a.go:1.1,2.1 1 3\n")) {
		t.Fatal("merge failed for mode atomic")
	}
	if got, _ := os.ReadFile(atomicOut); !strings.Contains(string(got), "m/a.go:1.1,2.1 1 5") {
		t.Errorf("mode atomic must combine as sum (2+3=5), got:\n%s", got)
	}
}

func TestMergeCoverProfilesSkipsMissingSources(t *testing.T) {
	dir := t.TempDir()
	root := writeProfile(t, dir, "root.out", "mode: set\nm/a.go:1.1,2.1 1 1\n")

	// A repo with no submodules passes only the root profile; a submodule whose
	// run failed leaves no file. Neither is an error.
	out := filepath.Join(dir, "merged.out")
	if !mergeCoverProfiles(out, root, filepath.Join(dir, "does-not-exist.out")) {
		t.Fatal("a missing source must be skipped, not fail the merge")
	}
	if got, _ := os.ReadFile(out); !strings.Contains(string(got), "m/a.go:1.1,2.1 1 1") {
		t.Errorf("root profile lost, got:\n%s", got)
	}

	if mergeCoverProfiles(filepath.Join(dir, "empty.out"), filepath.Join(dir, "nope.out")) {
		t.Error("merging nothing must report false, not write an empty profile")
	}
}
