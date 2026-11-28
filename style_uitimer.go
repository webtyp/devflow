package devflow

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Import paths the interface-timer rule reads: a package that imports domPackage is interface code, and its timers
// must be dom.AfterFunc, never AfterFunc from one of timerPackages.
const domPackage = "webtyp.com/dom"

var timerPackages = []string{"webtyp.com/time", "time"}

// uiTimerAnalyzer reports a timer from webtyp.com/time (or the standard library) in a package that draws with
// webtyp.com/dom. On the server that timer runs its callback on another goroutine, beside the code drawing with the
// same signals — a data race the browser never shows. dom.AfterFunc runs on the interface's thread.
var uiTimerAnalyzer = &analysis.Analyzer{
	Name:     "uitimer",
	Doc:      "reports time.AfterFunc in packages that use webtyp.com/dom (use dom.AfterFunc)",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runUITimer,
}

func runUITimer(pass *analysis.Pass) (any, error) {
	if !importsPackage(pass.Pkg, domPackage) {
		return nil, nil
	}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		var name *ast.Ident
		switch fun := ast.Unparen(call.Fun).(type) {
		case *ast.SelectorExpr:
			name = fun.Sel
		case *ast.Ident:
			name = fun
		default:
			return
		}
		fn, ok := pass.TypesInfo.Uses[name].(*types.Func)
		if !ok || fn.Name() != "AfterFunc" || fn.Pkg() == nil || !isTimerPackage(fn.Pkg().Path()) {
			return
		}
		pass.Reportf(call.Pos(), "this timer runs on another goroutine on the server, while the page is drawn with the same signals: use dom.AfterFunc, which runs on the interface's thread")
	})
	return nil, nil
}

func importsPackage(pkg *types.Package, path string) bool {
	for _, imp := range pkg.Imports() {
		if imp.Path() == path {
			return true
		}
	}
	return false
}

func isTimerPackage(path string) bool {
	for _, p := range timerPackages {
		if p == path {
			return true
		}
	}
	return false
}
