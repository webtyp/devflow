package devflow

import (
	"context"
	"fmt"
	"os"
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
		return out.String(), true, nil
	}
	return out.String(), false, nil
}
