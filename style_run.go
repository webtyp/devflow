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

// runStyle runs the style analyzers (QF, S, SA via `go vet -vettool=<this binary>`)
// and the unused analyzer (U1000 via `<this binary>`) over the module.
// It returns the combined output and whether the step failed. The error is set only
// when the gotest binary cannot be located.
func (g *Go) runStyle(runAll bool) (output string, failed bool, err error) {
	self, err := os.Executable()
	if err != nil {
		return "", true, fmt.Errorf("style: cannot locate the gotest binary: %w", err)
	}

	vetArgs := []string{"vet", styleFlagPrefix + self}
	var unusedArgs []string
	if runAll {
		vetArgs = append(vetArgs, "-tags=integration")
		unusedArgs = append(unusedArgs, "-tags=integration")
	}
	vetArgs = append(vetArgs, "./...")
	unusedArgs = append(unusedArgs, "./...")

	vetOut, vetFailed := g.runStyleCmd(styleToolEnv, "go", vetArgs...)
	unusedOut, unusedFailed := g.runStyleCmd(unusedToolEnv, self, unusedArgs...)

	if !vetFailed && !unusedFailed {
		return vetOut + unusedOut, false, nil
	}
	var combined strings.Builder
	if vetFailed {
		combined.WriteString(vetOut)
	}
	if unusedFailed {
		if combined.Len() > 0 && !strings.HasSuffix(vetOut, "\n") {
			combined.WriteByte('\n')
		}
		combined.WriteString(unusedOut)
	}
	return groupStyleFindings(combined.String()), true, nil
}

func (g *Go) runStyleCmd(envKey, name string, args ...string) (string, bool) {
	cmd := GoTestCmdFn(context.Background(), g.rootDir, name, args...)
	cmd.Env = append(os.Environ(), envKey+"=1")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if runErr := cmd.Run(); runErr != nil {
		if noPackagesToVet(out.String()) {
			return out.String(), false
		}
		return out.String(), true
	}
	return out.String(), false
}

// styleFinding matches one `file:line:col: message` line of the style tool.
var styleFinding = regexp.MustCompile(`^(.+?):(\d+):(\d+): (.*)$`)

type styleLocation struct {
	file      string
	line, col int
}

func (l styleLocation) String() string { return fmt.Sprintf("%s:%d:%d", l.file, l.line, l.col) }

// groupStyleFindings prints a message once with all its locations when it repeats,
// omitting the file name after its first mention:
//
//	could remove embedded field "Config" from selector in:
//	  emit_core.go:359:41, 360:45
//
// Different files are separated by "; ". A message with one location, and any line
// that is not a finding (package headers, tool errors), stays as the tool printed it.
// Messages keep the order in which they first appeared.
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
			b.WriteString(line)
			b.WriteByte('\n')
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
			b.WriteString(locs[0].String())
			b.WriteString(": ")
			b.WriteString(key)
			b.WriteByte('\n')
			continue
		}
		b.WriteString(key)
		b.WriteString(" in:\n  ")
		for i, l := range locs {
			if i == 0 {
				b.WriteString(l.String())
				continue
			}
			if l.file == locs[i-1].file {
				b.WriteString(", ")
				b.WriteString(strconv.Itoa(l.line))
				b.WriteByte(':')
				b.WriteString(strconv.Itoa(l.col))
				continue
			}
			b.WriteString("; ")
			b.WriteString(l.String())
		}
		b.WriteByte('\n')
	}
	return b.String()
}
