package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Keep subprocess integration tests from accidentally regressing to the
// user's conventional Mihomo Controller endpoint. Mock URLs must be passed
// through a variable created by httptest.Server instead.
func TestSubprocessIntegrationCommandsDoNotUseRealControllerEndpoint(t *testing.T) {
	paths, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("failed to enumerate Go tests: %v", err)
	}

	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Command" {
				return true
			}
			packageIdent, ok := selector.X.(*ast.Ident)
			if !ok || packageIdent.Name != "exec" {
				return true
			}
			for _, arg := range call.Args {
				literal, ok := arg.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err == nil && strings.Contains(value, "127.0.0.1:9090") {
					t.Errorf("%s passes the real Controller endpoint directly to exec.Command; use an httptest mock", path)
				}
			}
			return true
		})
	}
}
