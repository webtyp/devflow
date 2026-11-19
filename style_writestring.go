package devflow

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// writeStringAnalyzer flags inefficient string concatenation in calls to
// (*strings.Builder).WriteString and (*bytes.Buffer).WriteString, matching
// gopls's writestring analyzer. Constant concatenations are folded at compile
// time and are ignored.
var writeStringAnalyzer = &analysis.Analyzer{
	Name:     "writestring",
	Doc:      "reports inefficient string concatenation in calls to WriteString",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runWriteString,
}

func runWriteString(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if len(call.Args) != 1 {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "WriteString" {
			return
		}
		if !isBufferWriteString(pass.TypesInfo.Uses[sel.Sel]) {
			return
		}
		arg := ast.Unparen(call.Args[0])
		bin, ok := arg.(*ast.BinaryExpr)
		if !ok || bin.Op != token.ADD {
			return
		}
		if tv, ok := pass.TypesInfo.Types[arg]; ok && tv.Value != nil {
			return // compile-time constant concatenation: zero allocation
		}
		pass.Reportf(call.Pos(), "inefficient string concatenation in call to WriteString")
	})
	return nil, nil
}

func isBufferWriteString(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := sig.Recv().Type()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	pkg := named.Obj().Pkg().Path()
	name := named.Obj().Name()
	return (pkg == "strings" && name == "Builder") || (pkg == "bytes" && name == "Buffer")
}
