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

func TestRuntimeLifecycleToolsRequireMockControllerAndExactPidCleanup(t *testing.T) {
	lifecycleDir := filepath.Join("..", "..", "tools", "runtime")
	entries, err := os.ReadDir(lifecycleDir)
	if err != nil {
		t.Fatalf("failed to enumerate Runtime lifecycle tools: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mjs") {
			continue
		}
		path := filepath.Join(lifecycleDir, entry.Name())
		sourceBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}
		source := string(sourceBytes)
		for _, endpoint := range forbiddenControllerEndpoints() {
			if strings.Contains(source, endpoint) {
				t.Errorf("%s contains forbidden Controller endpoint %s", path, endpoint)
			}
		}
		lower := strings.ToLower(source)
		for _, broadKill := range []string{"taskkill", "killall", "pkill"} {
			if strings.Contains(lower, broadKill) {
				t.Errorf("%s contains broad process cleanup command %q; use exact recorded test PIDs", path, broadKill)
			}
		}
		for _, required := range []string{
			"assertMockControllerUrl",
			"PROXYLENS_CONTROLLER_URL",
			"mkdtempSync",
			"stopExactProcess",
			"stopExactPid",
		} {
			if !strings.Contains(source, required) {
				t.Errorf("%s is missing required lifecycle safety marker %q", path, required)
			}
		}
	}
}

func TestSupervisorLifecycleToolUsesOnlyE2ESafeStateAndCredentials(t *testing.T) {
	path := filepath.Join("..", "..", "tools", "runtime", "run-phase3e2b1-supervisor.mjs")
	sourceBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	source := string(sourceBytes)
	for _, required := range []string{
		"PROXYLENS_E2E_MODE",
		"PROXYLENS_E2E_CREDENTIAL_TARGET",
		"PROXYLENS_E2E_STATUS_FILE",
		"crypto.randomUUID",
		"config",
		"set-secret",
		"clear-secret",
		"delete environment.MIHOMO_SECRET",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("%s is missing required E2E safety marker %q", path, required)
		}
	}
	if strings.Contains(source, "ProxyLens/MihomoController/v1") {
		t.Errorf("%s names the production Credential Manager target", path)
	}
	if strings.Contains(source, "--secret") {
		t.Errorf("%s passes or accepts a secret through argv", path)
	}
	if strings.Contains(source, "MIHOMO_SECRET =") {
		t.Errorf("%s assigns a secret to MIHOMO_SECRET instead of deleting inherited state", path)
	}
}

func TestSettingsProductHarnessReestablishesOnlyIsolatedRuntimeState(t *testing.T) {
	path := filepath.Join("..", "..", "tools", "runtime", "run-phase3e2b2b-settings-product.mjs")
	sourceBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	source := string(sourceBytes)
	for _, required := range []string{
		"PROXYLENS_DB_PATH",
		"PROXYLENS_CONFIG_DIR",
		"PROXYLENS_E2E_CREDENTIAL_TARGET",
		"PROXYLENS_E2E_TASK_NAME",
		"PROXYLENS_E2E_TASK_ARGS",
		"task-owner-wrapper.cmd",
		"config', 'apply",
		"autostart=false->true",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("%s is missing isolated Settings acceptance marker %q", path, required)
		}
	}
	for _, forbidden := range []string{
		"delete baseEnvironment.PROXYLENS_DB_PATH",
		"ProxyLens/MihomoController/v1",
		"PROXYLENS_E2E_TASK_SCHEDULE",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("%s contains unsafe or obsolete marker %q", path, forbidden)
		}
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
