package devflow

import (
	"fmt"

	"webtyp.com/context"
	"webtyp.com/mcp"
	"webtyp.com/model"
)

// GoTestProvider exposes the gotest suite as a single MCP tool.
type GoTestProvider struct {
	g *Go
}

// NewGoTestProvider creates a new GoTestProvider.
func NewGoTestProvider(g *Go) *GoTestProvider {
	return &GoTestProvider{g: g}
}

var GoTestArgsModel = model.Definition{
	Name: "go_test_args",
	Fields: model.Fields{
		{Name: "run", Type: model.Text()},
	},
}

// GoTestArgs are the arguments accepted by the run_tests MCP tool.
type GoTestArgs struct {
	Run string
}

func (m *GoTestArgs) ModelName() string { return "go_test_args" }

func (m *GoTestArgs) Schema() []model.Field { return GoTestArgsModel.Fields }

func (m *GoTestArgs) Pointers() []any { return []any{&m.Run} }

func (m *GoTestArgs) IsNil() bool { return m == nil }

func (m *GoTestArgs) EncodeFields(w model.FieldWriter) {
	w.String("run", m.Run)
}

func (m *GoTestArgs) DecodeFields(r model.FieldReader) {
	if v, ok := r.String("run"); ok {
		m.Run = v
	}
}

func (m *GoTestArgs) Validate(action byte) error {
	return model.ValidateFields(action, m)
}

// Tools implements mcp.ToolProvider.
func (p *GoTestProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "run_tests",
			Description: "Comprehensive Go test suite: runs vet, stdlib tests with race detection, exact " +
				"coverage analysis, and auto-detected WASM tests. Full suite (no args) includes badges update " +
				"and slow test detection. Fast path (with -run/flags) skips vet and badges. Intelligent caching " +
				"by git state; cache disabled with custom flags.",
			Args:     new(GoTestArgs),
			Resource: "tests",
			Action:   'r',
			Execute:  p.execute,
		},
	}
}

func (p *GoTestProvider) execute(_ *context.Context, req mcp.Request) (*mcp.Result, error) {
	var args GoTestArgs
	if req.Params.Arguments != "" {
		if err := req.Bind(&args); err != nil {
			return nil, err
		}
	}

	var summary string
	var err error

	if args.Run == "" {
		summary, err = p.g.Test(nil, false, 0, false, false)
	} else {
		summary, err = p.g.Test([]string{"-run", args.Run}, false, 0, false, false)
	}

	text := summary
	if err != nil {
		text += fmt.Sprintf("\nError: %v", err)
	}
	return mcp.Text(text), nil
}
