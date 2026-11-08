package devflow

import (
	"os"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"
	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/quickfix"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
)

// styleToolEnv, when set, makes the gotest binary behave as a `go vet -vettool`
// running the style analyzers instead of the CLI.
const styleToolEnv = "GOTEST_STYLE_VETTOOL"

// styleFlagPrefix is the argument that points `go vet` to the tool binary.
const styleFlagPrefix = "-vettool="

// MaybeRunStyleTool turns the process into the style vet tool when styleToolEnv is
// set; otherwise it returns at once. Call it first thing in main (and in TestMain).
// Example: func main() { devflow.MaybeRunStyleTool(); … }
func MaybeRunStyleTool() {
	if os.Getenv(styleToolEnv) == "" {
		return
	}
	unitchecker.Main(styleAnalyzers()...)
}

// styleAnalyzers lists SA (bugs), S (simplifications) and QF (quick fixes).
// ST (naming and comment style) is deliberately absent.
func styleAnalyzers() []*analysis.Analyzer {
	var all []*analysis.Analyzer
	for _, group := range [][]*lint.Analyzer{quickfix.Analyzers, simple.Analyzers, staticcheck.Analyzers} {
		for _, l := range group {
			all = append(all, l.Analyzer)
		}
	}
	return all
}
