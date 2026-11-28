package devflow

import (
	"os"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"
	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/lintcmd"
	"honnef.co/go/tools/quickfix"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/unused"
)

// styleToolEnv, when set, makes the gotest binary behave as a `go vet -vettool`
// running the style analyzers instead of the CLI.
const styleToolEnv = "GOTEST_STYLE_VETTOOL"

// unusedToolEnv, when set, makes the gotest binary run staticcheck's unused (U1000)
// analyzer across the module instead of the CLI.
const unusedToolEnv = "GOTEST_UNUSED_TOOL"

// styleFlagPrefix is the argument that points `go vet` to the tool binary.
const styleFlagPrefix = "-vettool="

// MaybeRunStyleTool turns the process into the style vet tool (or unused analyzer) when
// its env var is set; otherwise it returns at once. Call it first thing in main (and in TestMain).
// Example: func main() { devflow.MaybeRunStyleTool(); … }
func MaybeRunStyleTool() {
	if os.Getenv(unusedToolEnv) != "" {
		cmd := lintcmd.NewCommand("gotest")
		cmd.AddAnalyzers(unused.Analyzer)
		cmd.ParseFlags(os.Args[1:])
		cmd.Run()
	}
	if os.Getenv(styleToolEnv) == "" {
		return
	}
	unitchecker.Main(styleAnalyzers()...)
}

// styleAnalyzers lists SA (bugs), S (simplifications), QF (quick fixes),
// and writestring (inefficient WriteString concatenation).
// ST (naming and comment style) is deliberately absent.
func styleAnalyzers() []*analysis.Analyzer {
	all := []*analysis.Analyzer{writeStringAnalyzer, uiTimerAnalyzer}
	for _, group := range [][]*lint.Analyzer{quickfix.Analyzers, simple.Analyzers, staticcheck.Analyzers} {
		for _, l := range group {
			all = append(all, l.Analyzer)
		}
	}
	return all
}
