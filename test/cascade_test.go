package devflow_test

// Tests for the transitive cascade coordinator (PLAN.md Fase 3).
// Contract under test:
//   - BuildDependentGraph: transitive closure of dependents, topologically
//     ordered, cycle detection, MaxCascadeDepth limit.
//   - RunCascade: processes each node EXACTLY ONCE per wave with ALL the bumps
//     of its already-published in-cascade dependencies; a node with zero
//     available bumps (all upstreams failed/unpublished) is skipped; failures
//     cut only their own branch.
//   - The real node processor (bump+test+commit+tag+push) is injected via
//     SetCascadeProcessFn so these tests need no network, git, nor toolchain.
// These tests define the target contract; the implementation must make them
// pass WITHOUT modifying the expectations.

import (
	"fmt"
	gitmod "webtyp.com/git"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"webtyp.com/devflow"
)

// testWriteModule creates <tmp>/<name>/go.mod for module github.com/test/<name>
// requiring the given sibling modules.
func testWriteModule(t *testing.T, tmp, name string, requires ...string) string {
	t.Helper()
	dir := filepath.Join(tmp, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "module github.com/test/%s\n\ngo 1.20\n", name)
	for _, r := range requires {
		fmt.Fprintf(&b, "\nrequire github.com/test/%s v0.0.1\n", r)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newCascadeHandler(t *testing.T, rootDir string) *devflow.Go {
	t.Helper()
	mockGit := &MockGitClient{}
	g := newGoHandlerWithMockBackup(t, mockGit)
	g.SetRootDir(rootDir)
	g.SetConsoleOutput(func(string) {})
	return g
}

func TestMaxCascadeDepthConstant(t *testing.T) {
	if devflow.MaxCascadeDepth != 10 {
		t.Errorf("MaxCascadeDepth must be 10, got %d", devflow.MaxCascadeDepth)
	}
}

func TestBuildDependentGraph_ChainTopologicalOrder(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "main")
	testWriteModule(t, tmp, "b", "main")
	testWriteModule(t, tmp, "c", "b")
	testWriteModule(t, tmp, "indep") // must not appear

	g := newCascadeHandler(t, mainDir)

	nodes, err := g.BuildDependentGraph("github.com/test/main", tmp)
	if err != nil {
		t.Fatalf("BuildDependentGraph: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("expected 2 transitive dependents (b, c), got %d: %+v", len(nodes), nodes)
	}

	idx := map[string]int{}
	for i, n := range nodes {
		idx[n.ModulePath] = i
	}
	if _, ok := idx["github.com/test/b"]; !ok {
		t.Fatal("graph must include direct dependent b")
	}
	if _, ok := idx["github.com/test/c"]; !ok {
		t.Fatal("graph must include TRANSITIVE dependent c (this is the whole point of the cascade)")
	}
	if idx["github.com/test/b"] > idx["github.com/test/c"] {
		t.Error("topological order violated: b must come before c (c depends on b)")
	}
}

func TestBuildDependentGraph_DiamondSingleNode(t *testing.T) {
	// main ← b, main ← c, b ← d, c ← d
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "main")
	testWriteModule(t, tmp, "b", "main")
	testWriteModule(t, tmp, "c", "main")
	testWriteModule(t, tmp, "d", "b", "c")

	g := newCascadeHandler(t, mainDir)

	nodes, err := g.BuildDependentGraph("github.com/test/main", tmp)
	if err != nil {
		t.Fatalf("BuildDependentGraph: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes (b, c, d — d appears ONCE), got %d: %+v", len(nodes), nodes)
	}

	idx := map[string]int{}
	for i, n := range nodes {
		idx[n.ModulePath] = i
	}
	if idx["github.com/test/d"] < idx["github.com/test/b"] || idx["github.com/test/d"] < idx["github.com/test/c"] {
		t.Error("topological order violated: d must come after both b and c")
	}
}

func TestBuildDependentGraph_CycleIsAnError(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "main")
	testWriteModule(t, tmp, "b", "main", "c")
	testWriteModule(t, tmp, "c", "b")

	g := newCascadeHandler(t, mainDir)

	_, err := g.BuildDependentGraph("github.com/test/main", tmp)
	if err == nil {
		t.Fatal("a dependency cycle (b ↔ c) must be an error — publishing any of it would loop")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "cycle") {
		t.Errorf("error must mention the cycle, got: %v", err)
	}
}

func TestBuildDependentGraph_DepthLimit(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "m0")
	// Chain of 11 dependent levels: m1←m2←…←m11 (> MaxCascadeDepth)
	prev := "m0"
	for i := 1; i <= devflow.MaxCascadeDepth+1; i++ {
		name := fmt.Sprintf("m%d", i)
		testWriteModule(t, tmp, name, prev)
		prev = name
	}

	g := newCascadeHandler(t, mainDir)

	_, err := g.BuildDependentGraph("github.com/test/m0", tmp)
	if err == nil {
		t.Fatal("exceeding MaxCascadeDepth must be an error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "depth") {
		t.Errorf("error must mention the depth limit, got: %v", err)
	}
}

func TestRunCascade_ChainPropagatesBumpsAndCause(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "main")
	testWriteModule(t, tmp, "b", "main")
	testWriteModule(t, tmp, "c", "b")

	g := newCascadeHandler(t, mainDir)

	var mu sync.Mutex
	calls := map[string][]gitmod.DepBump{}
	causes := map[string]string{}
	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		mu.Lock()
		defer mu.Unlock()
		calls[node.ModulePath] = bumps
		causes[node.ModulePath] = rootCause
		return devflow.CascadeOutcome{Status: devflow.CascadeStatusPublished, Version: "v9.9.9"}, nil // every node publishes
	})

	report := g.RunCascade("github.com/test/main", "v1.0.0", "feat: cause raíz", tmp)

	mu.Lock()
	defer mu.Unlock()

	// b receives exactly the root bump
	bBumps := calls["github.com/test/b"]
	if len(bBumps) != 1 || bBumps[0].ModulePath != "github.com/test/main" || bBumps[0].NewVersion != "v1.0.0" {
		t.Errorf("b must be bumped with main v1.0.0, got: %+v", bBumps)
	}

	// c receives b's freshly published version — the transitive propagation
	cBumps := calls["github.com/test/c"]
	if len(cBumps) != 1 || cBumps[0].ModulePath != "github.com/test/b" || cBumps[0].NewVersion != "v9.9.9" {
		t.Errorf("c must be bumped with b v9.9.9 (published in this wave), got: %+v", cBumps)
	}

	// The root cause travels to every node
	for mod, cause := range causes {
		if cause != "feat: cause raíz" {
			t.Errorf("root cause must propagate to %s, got: %q", mod, cause)
		}
	}

	// Report: both published
	if len(report.Entries) != 2 {
		t.Fatalf("expected 2 report entries, got %d: %+v", len(report.Entries), report.Entries)
	}
	for _, e := range report.Entries {
		if e.Status != devflow.CascadeStatusPublished {
			t.Errorf("%s expected status %q, got %q (%s)", e.ModulePath, devflow.CascadeStatusPublished, e.Status, e.Detail)
		}
	}
}

func TestRunCascade_DiamondProcessesNodeOnceWithAllBumps(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "main")
	testWriteModule(t, tmp, "b", "main")
	testWriteModule(t, tmp, "c", "main")
	testWriteModule(t, tmp, "d", "b", "c")

	g := newCascadeHandler(t, mainDir)

	var mu sync.Mutex
	callCount := map[string]int{}
	calls := map[string][]gitmod.DepBump{}
	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		mu.Lock()
		defer mu.Unlock()
		callCount[node.ModulePath]++
		calls[node.ModulePath] = bumps
		return devflow.CascadeOutcome{Status: devflow.CascadeStatusPublished, Version: "v9.9.9"}, nil
	})

	g.RunCascade("github.com/test/main", "v1.0.0", "", tmp)

	mu.Lock()
	defer mu.Unlock()

	if callCount["github.com/test/d"] != 1 {
		t.Fatalf("d must be processed exactly ONCE (one commit+tag), got %d calls", callCount["github.com/test/d"])
	}
	dBumps := calls["github.com/test/d"]
	if len(dBumps) != 2 {
		t.Fatalf("d must receive BOTH bumps (b and c) in a single call, got: %+v", dBumps)
	}
	got := map[string]bool{}
	for _, bump := range dBumps {
		got[bump.ModulePath] = true
	}
	if !got["github.com/test/b"] || !got["github.com/test/c"] {
		t.Errorf("d's bumps must cover b and c, got: %+v", dBumps)
	}
}

func TestRunCascade_FailureCutsOnlyItsBranch(t *testing.T) {
	// main ← b (fails), main ← c, b ← x (only dep is b → skipped),
	// b ← d, c ← d (d still processed with c's bump only)
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "main")
	testWriteModule(t, tmp, "b", "main")
	testWriteModule(t, tmp, "c", "main")
	testWriteModule(t, tmp, "x", "b")
	testWriteModule(t, tmp, "d", "b", "c")

	g := newCascadeHandler(t, mainDir)

	var mu sync.Mutex
	calls := map[string][]gitmod.DepBump{}
	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		mu.Lock()
		calls[node.ModulePath] = bumps
		mu.Unlock()
		if node.ModulePath == "github.com/test/b" {
			return devflow.CascadeOutcome{}, fmt.Errorf("tests failed")
		}
		return devflow.CascadeOutcome{Status: devflow.CascadeStatusPublished, Version: "v9.9.9"}, nil
	})

	report := g.RunCascade("github.com/test/main", "v1.0.0", "", tmp)

	mu.Lock()
	defer mu.Unlock()

	// x must NOT be processed: its only in-cascade dep (b) failed
	if _, called := calls["github.com/test/x"]; called {
		t.Error("x must be skipped — its only upstream (b) failed, there is nothing to bump")
	}

	// d IS processed, but only with c's bump (partial update is safe:
	// d simply stays on the old b version)
	dBumps := calls["github.com/test/d"]
	if len(dBumps) != 1 || dBumps[0].ModulePath != "github.com/test/c" {
		t.Errorf("d must receive only c's bump, got: %+v", dBumps)
	}

	// Report statuses
	status := map[string]string{}
	for _, e := range report.Entries {
		status[e.ModulePath] = e.Status
	}
	if status["github.com/test/b"] != devflow.CascadeStatusFailed {
		t.Errorf("b expected %q, got %q", devflow.CascadeStatusFailed, status["github.com/test/b"])
	}
	if status["github.com/test/x"] != devflow.CascadeStatusSkipped {
		t.Errorf("x expected %q, got %q", devflow.CascadeStatusSkipped, status["github.com/test/x"])
	}
	if status["github.com/test/c"] != devflow.CascadeStatusPublished {
		t.Errorf("c expected %q, got %q", devflow.CascadeStatusPublished, status["github.com/test/c"])
	}
	if status["github.com/test/d"] != devflow.CascadeStatusPublished {
		t.Errorf("d expected %q, got %q", devflow.CascadeStatusPublished, status["github.com/test/d"])
	}
}

// A node whose processor returns ("", nil) made progress without publishing a
// version (e.g. the dirty-tree deps-only path): downstream nodes get no bump
// from it.
func TestRunCascade_DepsOnlyNodeDoesNotPropagate(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "main")
	testWriteModule(t, tmp, "b", "main")
	testWriteModule(t, tmp, "c", "b")

	g := newCascadeHandler(t, mainDir)

	var mu sync.Mutex
	calls := map[string][]gitmod.DepBump{}
	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		mu.Lock()
		calls[node.ModulePath] = bumps
		mu.Unlock()
		if node.ModulePath == "github.com/test/b" {
			return devflow.CascadeOutcome{Status: devflow.CascadeStatusDepsOnly}, nil // deps-only: committed bump, no tag
		}
		return devflow.CascadeOutcome{Status: devflow.CascadeStatusPublished, Version: "v9.9.9"}, nil
	})

	report := g.RunCascade("github.com/test/main", "v1.0.0", "", tmp)

	mu.Lock()
	defer mu.Unlock()

	if _, called := calls["github.com/test/c"]; called {
		t.Error("c must be skipped — b published no new version to bump to")
	}

	status := map[string]string{}
	for _, e := range report.Entries {
		status[e.ModulePath] = e.Status
	}
	if status["github.com/test/b"] != devflow.CascadeStatusDepsOnly {
		t.Errorf("b expected %q, got %q", devflow.CascadeStatusDepsOnly, status["github.com/test/b"])
	}
	if status["github.com/test/c"] != devflow.CascadeStatusSkipped {
		t.Errorf("c expected %q, got %q", devflow.CascadeStatusSkipped, status["github.com/test/c"])
	}
}

func TestRunCascade_SkippedNodeDoesNotPropagate(t *testing.T) {
	tmp := t.TempDir()
	mainDir := testWriteModule(t, tmp, "main")
	testWriteModule(t, tmp, "b", "main")
	testWriteModule(t, tmp, "c", "b")

	g := newCascadeHandler(t, mainDir)

	var mu sync.Mutex
	calls := map[string][]gitmod.DepBump{}
	g.SetCascadeProcessFn(func(node devflow.CascadeNode, bumps []gitmod.DepBump, rootCause string) (devflow.CascadeOutcome, error) {
		mu.Lock()
		calls[node.ModulePath] = bumps
		mu.Unlock()
		if node.ModulePath == "github.com/test/b" {
			return devflow.CascadeOutcome{Status: devflow.CascadeStatusSkipped, Reason: "codejob session active"}, nil
		}
		return devflow.CascadeOutcome{Status: devflow.CascadeStatusPublished, Version: "v9.9.9"}, nil
	})

	report := g.RunCascade("github.com/test/main", "v1.0.0", "", tmp)

	mu.Lock()
	defer mu.Unlock()

	if _, called := calls["github.com/test/c"]; called {
		t.Error("c must be skipped — b was skipped and published no new version")
	}

	status := map[string]string{}
	details := map[string]string{}
	for _, e := range report.Entries {
		status[e.ModulePath] = e.Status
		details[e.ModulePath] = e.Detail
	}
	if status["github.com/test/b"] != devflow.CascadeStatusSkipped {
		t.Errorf("b expected %q, got %q", devflow.CascadeStatusSkipped, status["github.com/test/b"])
	}
	if details["github.com/test/b"] != "codejob session active" {
		t.Errorf("b detail expected %q, got %q", "codejob session active", details["github.com/test/b"])
	}
	if status["github.com/test/c"] != devflow.CascadeStatusSkipped {
		t.Errorf("c expected %q, got %q", devflow.CascadeStatusSkipped, status["github.com/test/c"])
	}
	if details["github.com/test/c"] != "no upstream bumps" {
		t.Errorf("c detail expected %q, got %q", "no upstream bumps", details["github.com/test/c"])
	}
}
