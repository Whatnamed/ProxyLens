package test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func forbiddenControllerEndpoints() []string {
	return []string{
		strings.Join([]string{"127.0.0.1", "9090"}, ":"),
		strings.Join([]string{"localhost", "9090"}, ":"),
		strings.Join([]string{"127.0.0.1", "7988"}, ":"),
		strings.Join([]string{"localhost", "7988"}, ":"),
	}
}

// Keep subprocess integration tests from accidentally regressing to the
// user's conventional Mihomo Controller endpoint. Mock URLs must be passed
// through a variable created by httptest.Server instead.
func TestSubprocessIntegrationCommandsDoNotUseRealControllerEndpoint(t *testing.T) {
	var paths []string
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to enumerate Go tests: %v", err)
	}

	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", path, err)
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}
		for _, endpoint := range forbiddenControllerEndpoints() {
			if strings.Contains(string(source), endpoint) {
				t.Errorf("%s contains the real Controller endpoint %s; use an httptest mock", path, endpoint)
			}
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
			hasRunSubcommand := false
			hasExplicitController := false
			for index, arg := range call.Args {
				literal, ok := arg.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err == nil && containsForbiddenControllerEndpoint(value) {
					t.Errorf("%s passes the real Controller endpoint directly to exec.Command; use an httptest mock", path)
				}
				if index > 0 && value == "run" {
					hasRunSubcommand = true
				}
				if index > 0 && (value == "--controller" || strings.HasPrefix(value, "--controller=")) {
					hasExplicitController = true
				}
			}
			if hasRunSubcommand && !hasExplicitController {
				t.Errorf("%s invokes a collector run subprocess without an explicit --controller; use an httptest mock URL", path)
			}
			return true
		})
	}
}

func containsForbiddenControllerEndpoint(value string) bool {
	for _, endpoint := range forbiddenControllerEndpoints() {
		if strings.Contains(value, endpoint) {
			return true
		}
	}
	return false
}
