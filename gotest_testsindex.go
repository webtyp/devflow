package devflow

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TestsIndexFile is the domain tests index every repo keeps, relative to its root.
const TestsIndexFile = "docs/TESTS.md"

const (
	testsIndexLabel   = "tests index"
	testsIndexMissing = "⚠️ docs/TESTS.md missing: domain tests are not indexed"
	missingRowFormat  = "docs/TESTS.md: missing row for %s (%s)"
	orphanRowFormat   = "docs/TESTS.md: row %s → %s: test not found"
)

var testsIndexRow = regexp.MustCompile(`\[(Test[A-Za-z0-9_]*)\]\(([^)#\s]+_test\.go)(#[^)]*)?\)`)

// TestsIndexReport is the result of checking docs/TESTS.md against the test functions of a repo.
type TestsIndexReport struct {
	Present  bool     // docs/TESTS.md exists
	Problems []string // verbatim messages, sorted; empty means the index is in sync
}

// CheckTestsIndex compares docs/TESTS.md under rootDir with every Test function of the repo.
// Example: r, err := devflow.CheckTestsIndex("."); if r.Present && len(r.Problems) > 0 { ... }
func CheckTestsIndex(rootDir string) (TestsIndexReport, error) {
	content, err := os.ReadFile(filepath.Join(rootDir, TestsIndexFile))
	if err != nil {
		if os.IsNotExist(err) {
			return TestsIndexReport{}, nil
		}
		return TestsIndexReport{}, err
	}

	report := TestsIndexReport{Present: true}
	tests := make(map[string]bool)

	fset := token.NewFileSet()
	err = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel := filepath.ToSlash(filepath.Clean(path))
		if rootDir != "." && rootDir != "" {
			rel, _ = filepath.Rel(rootDir, path)
			rel = filepath.ToSlash(rel)
		}

		if path != rootDir && d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "vendor" || name == "testdata" || name == "node_modules" {
				return filepath.SkipDir
			}
			if checkFileExists(filepath.Join(path, "go.mod")) {
				return filepath.SkipDir
			}
		}

		if !d.IsDir() && strings.HasSuffix(path, "_test.go") {
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}

			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					if fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") && fn.Name.Name != "TestMain" && fn.Type.Params != nil && len(fn.Type.Params.List) == 1 {
						key := rel + "\x00" + fn.Name.Name
						tests[key] = true
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return TestsIndexReport{}, err
	}

	rows := make(map[string]bool)
	matches := testsIndexRow.FindAllStringSubmatch(string(content), -1)
	for _, m := range matches {
		testName := m[1]
		targetPath := m[2]

		resolvedRel := filepath.ToSlash(filepath.Clean(filepath.Join("docs", targetPath)))
		key := resolvedRel + "\x00" + testName
		rows[key] = true
	}

	type problem struct {
		rel  string
		name string
	}
	var missing []problem
	var orphans []problem

	for k := range tests {
		if !rows[k] {
			parts := strings.SplitN(k, "\x00", 2)
			missing = append(missing, problem{rel: parts[0], name: parts[1]})
		}
	}

	for k := range rows {
		if !tests[k] {
			parts := strings.SplitN(k, "\x00", 2)
			orphans = append(orphans, problem{rel: parts[0], name: parts[1]})
		}
	}

	sort.Slice(missing, func(i, j int) bool {
		if missing[i].rel == missing[j].rel {
			return missing[i].name < missing[j].name
		}
		return missing[i].rel < missing[j].rel
	})

	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].rel == orphans[j].rel {
			return orphans[i].name < orphans[j].name
		}
		return orphans[i].rel < orphans[j].rel
	})

	for _, p := range missing {
		report.Problems = append(report.Problems, fmt.Sprintf(missingRowFormat, p.name, p.rel))
	}
	for _, p := range orphans {
		report.Problems = append(report.Problems, fmt.Sprintf(orphanRowFormat, p.name, p.rel))
	}

	return report, nil
}
