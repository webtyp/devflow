package devflow

import (
	"fmt"
	gitmod "webtyp.com/git"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const MaxCascadeDepth = 10

// CascadeNode represents a module in the dependency graph
type CascadeNode struct {
	Dir        string
	ModulePath string
	DependsOn  []string // List of ModulePaths this node depends on *within the cascade*
}

// CascadeEntry represents the result for a single module in the cascade
type CascadeEntry struct {
	ModulePath string
	Status     string
	Detail     string
}

// CascadeOutcome is the typed result of processing one node. It replaces the
// previous convention of encoding the status inside a free-form string.
type CascadeOutcome struct {
	Status  string // CascadeStatusPublished | CascadeStatusDepsOnly | CascadeStatusSkipped
	Version string // set only when Status == CascadeStatusPublished
	Reason  string // human-readable, e.g. "codejob session active"
}

// CascadeReport contains the full report of the cascade execution
type CascadeReport struct {
	Entries []CascadeEntry
}

// CascadeProcessFn is the signature for the function that processes a single node
type CascadeProcessFn func(node CascadeNode, bumps []gitmod.DepBump, rootCause string) (CascadeOutcome, error)

// cascadeProcessFn stores the current processor
var cascadeProcessFn CascadeProcessFn

// SetCascadeProcessFn sets the function used to process each node in the cascade.
// This is used to inject mocks in tests.
func (g *Go) SetCascadeProcessFn(fn CascadeProcessFn) {
	cascadeProcessFn = fn
}

// BuildDependentGraph finds all modules that transitively depend on rootModule.
// It returns them in topological order.
func (g *Go) BuildDependentGraph(rootModule, searchPath string) ([]CascadeNode, error) {
	// 1. Find all modules in searchPath
	allModules, err := g.findAllModules(searchPath)
	if err != nil {
		return nil, err
	}

	// 2. Build adjacency list of who depends on whom
	// dependsOn[A] = [B, C] means A depends on B and C
	dependsOn := make(map[string][]string)
	moduleToDir := make(map[string]string)

	for dir, modPath := range allModules {
		moduleToDir[modPath] = dir
		deps, err := g.getModuleDependencies(dir)
		if err != nil {
			continue // Skip broken modules
		}
		dependsOn[modPath] = deps
	}

	// 3. Find transitive closure of dependents starting from rootModule
	// dependentsOf[A] = [B, C] means B and C depend on A
	dependentsOf := make(map[string][]string)
	for mod, deps := range dependsOn {
		for _, dep := range deps {
			dependentsOf[dep] = append(dependentsOf[dep], mod)
		}
	}

	visited := make(map[string]bool)
	var transitive []string
	var collect func(string, int) error
	collect = func(m string, depth int) error {
		if depth > MaxCascadeDepth {
			return fmt.Errorf("MaxCascadeDepth exceeded")
		}
		for _, dep := range dependentsOf[m] {
			if !visited[dep] {
				visited[dep] = true
				transitive = append(transitive, dep)
				if err := collect(dep, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// DO NOT set visited[rootModule] = true because we want to find its dependents
	if err := collect(rootModule, 0); err != nil {
		return nil, err
	}

	// 4. Topological sort of the transitive set
	// We only care about dependencies WITHIN the transitive set + rootModule
	inSet := make(map[string]bool)
	for _, m := range transitive {
		inSet[m] = true
	}
	inSet[rootModule] = true

	// Build nodes for the sort
	var nodes []CascadeNode
	for _, m := range transitive {
		var depsInSet []string
		for _, d := range dependsOn[m] {
			if inSet[d] {
				depsInSet = append(depsInSet, d)
			}
		}
		nodes = append(nodes, CascadeNode{
			Dir:        moduleToDir[m],
			ModulePath: m,
			DependsOn:  depsInSet,
		})
	}

	sorted, err := topologicalSort(nodes)
	if err != nil {
		return nil, err
	}

	return sorted, nil
}

func topologicalSort(nodes []CascadeNode) ([]CascadeNode, error) {
	nodeMap := make(map[string]CascadeNode)
	for _, n := range nodes {
		nodeMap[n.ModulePath] = n
	}

	// Calculate in-degrees (within the subgraph)
	inDegree := make(map[string]int)
	for _, n := range nodes {
		for _, dep := range n.DependsOn {
			// We only care about dependencies that are also in our nodes list
			if _, ok := nodeMap[dep]; ok {
				inDegree[n.ModulePath]++
			}
		}
	}

	var queue []string
	for _, n := range nodes {
		if inDegree[n.ModulePath] == 0 {
			queue = append(queue, n.ModulePath)
		}
	}

	// Sort initial queue for determinism
	sort.Strings(queue)

	var result []CascadeNode
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]

		result = append(result, nodeMap[u])

		// For all nodes v such that there is an edge u -> v
		// In our graph, if v depends on u, then u -> v
		for _, v := range nodes {
			isDependent := false
			for _, d := range v.DependsOn {
				if d == u {
					isDependent = true
					break
				}
			}

			if isDependent {
				inDegree[v.ModulePath]--
				if inDegree[v.ModulePath] == 0 {
					// Insert into queue while maintaining sort for determinism
					idx := sort.SearchStrings(queue, v.ModulePath)
					queue = append(queue, "")
					copy(queue[idx+1:], queue[idx:])
					queue[idx] = v.ModulePath
				}
			}
		}
	}

	if len(result) != len(nodes) {
		// Cycle detected or logic error
		return nil, fmt.Errorf("cycle detected in dependency graph")
	}

	return result, nil
}

// RunCascade executes the topological cascade
func (g *Go) RunCascade(rootModule, rootVersion, rootCause, searchPath string) CascadeReport {
	nodes, err := g.BuildDependentGraph(rootModule, searchPath)
	if err != nil {
		return CascadeReport{Entries: []CascadeEntry{{ModulePath: rootModule, Status: CascadeStatusFailed, Detail: err.Error()}}}
	}

	report := CascadeReport{}
	// publishedVersions tracks what version each module published in this wave
	publishedVersions := make(map[string]string)
	publishedVersions[rootModule] = rootVersion

	// Use default processor if none set
	processor := cascadeProcessFn
	if processor == nil {
		processor = g.defaultCascadeProcessor
	}

	for _, node := range nodes {
		// Collect bumps available for this node
		var bumps []gitmod.DepBump
		for _, dep := range node.DependsOn {
			if ver, ok := publishedVersions[dep]; ok && ver != "" {
				bumps = append(bumps, gitmod.DepBump{ModulePath: dep, NewVersion: ver})
			}
		}

		if len(bumps) == 0 {
			report.Entries = append(report.Entries, CascadeEntry{ModulePath: node.ModulePath, Status: CascadeStatusSkipped, Detail: "no upstream bumps"})
			continue
		}

		outcome, err := processor(node, bumps, rootCause)
		if err != nil {
			report.Entries = append(report.Entries, CascadeEntry{ModulePath: node.ModulePath, Status: CascadeStatusFailed, Detail: err.Error()})
			continue
		}

		detail := outcome.Reason
		if outcome.Status == CascadeStatusPublished {
			detail = outcome.Version
			publishedVersions[node.ModulePath] = outcome.Version
		}

		report.Entries = append(report.Entries, CascadeEntry{
			ModulePath: node.ModulePath,
			Status:     outcome.Status,
			Detail:     detail,
		})
	}

	g.printCascadeReport(report)
	return report
}

func (g *Go) defaultCascadeProcessor(node CascadeNode, bumps []gitmod.DepBump, rootCause string) (CascadeOutcome, error) {
	return g.UpdateDependentModule(node.Dir, bumps, rootCause)
}

func (g *Go) printCascadeReport(report CascadeReport) {
	if len(report.Entries) == 0 {
		return
	}
	g.consoleOutput("\nCascade report:")
	g.consoleOutput("--------------------------------------------------")
	for _, e := range report.Entries {
		icon := "✅"
		if e.Status == CascadeStatusFailed {
			icon = "❌"
		} else if e.Status == CascadeStatusSkipped {
			icon = "⏭"
		} else if e.Status == CascadeStatusDepsOnly {
			icon = "⚠"
		}
		g.consoleOutput(fmt.Sprintf("%s %-30s %-10s %s", icon, e.ModulePath, e.Status, e.Detail))
	}
	g.consoleOutput("--------------------------------------------------")
}

// findAllModules finds all go.mod files in searchPath
func (g *Go) findAllModules(searchPath string) (map[string]string, error) {
	modules := make(map[string]string)
	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !info.IsDir() && info.Name() == "go.mod" {
			dir := filepath.Dir(path)
			// Avoid including the current rootDir if it's inside searchPath
			absDir, _ := filepath.Abs(dir)
			absRoot, _ := filepath.Abs(g.rootDir)
			if absDir == absRoot {
				return nil
			}

			goHandler, _ := NewGo(nil)
			goHandler.SetRootDir(dir)
			modPath, err := goHandler.GetModulePath()
			if err == nil {
				modules[dir] = modPath
			}
		}
		return nil
	})
	return modules, err
}

func (g *Go) getModuleDependencies(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil, err
	}

	var deps []string
	lines := strings.Split(string(data), "\n")
	inBlock := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "require (" {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}
		if inBlock {
			parts := strings.Fields(line)
			if len(parts) >= 1 {
				deps = append(deps, parts[0])
			}
			continue
		}
		if strings.HasPrefix(line, "require ") {
			parts := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(parts) >= 1 {
				deps = append(deps, parts[0])
			}
		}
	}
	return deps, nil
}
