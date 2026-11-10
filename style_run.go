package devflow

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// noPackagesToVet reports whether go's output only says the module has nothing to analyse
// (WASM-only modules). Shared by the vet and style steps.
func noPackagesToVet(output string) bool {
	return strings.Contains(output, "matched no packages") ||
		strings.Contains(output, "no packages to vet") ||
		strings.Contains(output, "build constraints exclude all Go files")
}

// runStyle runs the style analyzers over the module through `go vet -vettool=<this binary>`.
// It returns the tool output and whether the step failed. The error is set only when the
// gotest binary cannot be located.
func (g *Go) runStyle(runAll bool) (output string, failed bool, err error) {
	self, err := os.Executable()
	if err != nil {
		return "", true, fmt.Errorf("style: cannot locate the gotest binary: %w", err)
	}
	args := []string{"vet", styleFlagPrefix + self}
	if runAll {
		args = append(args, "-tags=integration")
	}
	args = append(args, "./...")

	cmd := GoTestCmdFn(context.Background(), g.rootDir, "go", args...)
	cmd.Env = append(os.Environ(), styleToolEnv+"=1")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if runErr := cmd.Run(); runErr != nil {
		if noPackagesToVet(out.String()) {
			return out.String(), false, nil
		}
		return groupStyleFindings(out.String()), true, nil
	}
	return out.String(), false, nil
}

// styleFinding matches one `file:line:col: message` line of the style tool.
var styleFinding = regexp.MustCompile(`^(.+?):(\d+):(\d+): (.*)$`)

type styleLocation struct {
	file      string
	line, col int
}

func (l styleLocation) String() string { return fmt.Sprintf("%s:%d:%d", l.file, l.line, l.col) }

// groupStyleFindings prints a message once with all its locations when it repeats:
//
//	could remove embedded field "Config" from selector in:
//	  emit_core.go:359:41, emit_core.go:360:45
//
// A message with one location, and any line that is not a finding (package headers, tool errors),
// stays exactly as the tool printed it. Messages keep the order in which they first appeared.
func groupStyleFindings(output string) string {
	var order []string // messages, or the raw line for non-findings
	locations := map[string][]styleLocation{}
	raw := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		m := styleFinding.FindStringSubmatch(line)
		if m == nil {
			key := "\x00raw:" + line
			order = append(order, key)
			raw[key] = line
			continue
		}
		msg := m[4]
		if _, seen := locations[msg]; !seen {
			order = append(order, msg)
		}
		l, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		locations[msg] = append(locations[msg], styleLocation{file: m[1], line: l, col: c})
	}

	var b strings.Builder
	for _, key := range order {
		if line, isRaw := raw[key]; isRaw && strings.HasPrefix(key, "\x00raw:") {
			b.WriteString(line + "\n")
			continue
		}
		locs := locations[key]
		sort.SliceStable(locs, func(i, j int) bool {
			if locs[i].file != locs[j].file {
				return locs[i].file < locs[j].file
			}
			if locs[i].line != locs[j].line {
				return locs[i].line < locs[j].line
			}
			return locs[i].col < locs[j].col
		})
		if len(locs) == 1 {
			b.WriteString(locs[0].String() + ": " + key + "\n")
			continue
		}
		parts := make([]string, len(locs))
		for i, l := range locs {
			parts[i] = l.String()
		}
		b.WriteString(key + " in:\n  " + strings.Join(parts, ", ") + "\n")
	}
	return b.String()
}
