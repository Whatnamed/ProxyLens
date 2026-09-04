package runtimeconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testControllerURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func TestResolveConfigPathSeparatesConfigAndDataDirectories(t *testing.T) {
	got, err := ResolveConfigPath(filepath.Join("C:", "fixture", "config"), filepath.Join("C:", "fixture", "local"))
	if err != nil {
		t.Fatalf("ResolveConfigPath override failed: %v", err)
	}
	want := filepath.Join("C:", "fixture", "config", RuntimeConfigFileName)
	if got != want {
		t.Fatalf("got config path %q, want %q", got, want)
	}
	defaultPath, err := ResolveConfigPath("", filepath.Join("C:", "fixture", "local"))
	if err != nil {
		t.Fatalf("ResolveConfigPath default failed: %v", err)
	}
	if defaultPath != filepath.Join("C:", "fixture", "local", "ProxyLens", "config", RuntimeConfigFileName) {
		t.Fatalf("unexpected default config path %q", defaultPath)
	}
}

func TestConfigRoundTripIsNonSensitiveAndAtomicOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", RuntimeConfigFileName)
	cfg := RuntimeConfig{SchemaVersion: RuntimeConfigSchemaVersion, ControllerURL: testControllerURL(43127)}
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config failed: %v", err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "phase3e2b1") {
		t.Fatalf("saved runtime config contains sensitive-looking data: %s", data)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if loaded != cfg {
		t.Fatalf("loaded config=%+v, want %+v", loaded, cfg)
	}
	overwrite := RuntimeConfig{SchemaVersion: RuntimeConfigSchemaVersion, ControllerURL: testControllerURL(43128)}
	if err := SaveConfig(path, overwrite); err != nil {
		t.Fatalf("atomic overwrite failed: %v", err)
	}
	loaded, err = LoadConfig(path)
	if err != nil || loaded != overwrite {
		t.Fatalf("after overwrite loaded=%+v err=%v, want %+v", loaded, err, overwrite)
	}
}

func TestMissingMalformedAndFutureConfigBehavior(t *testing.T) {
	missing, err := LoadConfig(filepath.Join(t.TempDir(), RuntimeConfigFileName))
	if err != nil || missing != DefaultRuntimeConfig() {
		t.Fatalf("missing config=%+v err=%v, want default v1", missing, err)
	}
	path := filepath.Join(t.TempDir(), RuntimeConfigFileName)
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"controllerUrl":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("malformed config unexpectedly loaded")
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":2,"controllerUrl":"http://example.test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), ErrUnsupportedConfigSchema.Error()) {
		t.Fatalf("future config error=%v, want unsupported schema", err)
	}
	if err := SaveConfig(path, RuntimeConfig{SchemaVersion: 2}); err == nil {
		t.Fatal("future config was accepted for write")
	}
}

func TestControllerURLValidation(t *testing.T) {
	for _, value := range []string{
		testControllerURL(43127),
		"https://controller.example.test:8443/",
	} {
		if err := ValidateControllerURL(value); err != nil {
			t.Errorf("valid Controller URL %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{
		"not-a-url",
		"http://",
		"http://user:pass@example.test",
		"http://example.test/path/not-base",
		"http://example.test?secret=bad",
	} {
		if err := ValidateControllerURL(value); err == nil {
			t.Errorf("unsafe Controller URL %q unexpectedly accepted", value)
		}
	}
}

func TestE2EControllerURLValidation(t *testing.T) {
	if err := ValidateE2EControllerURL(testControllerURL(43127)); err != nil {
		t.Fatalf("random loopback mock rejected: %v", err)
	}
	for _, value := range []string{
		testControllerURL(9090),
		testControllerURL(7988),
		"http://127.0.0.1",
		"http://127.0.0.1:0",
		"http://localhost:43127",
		"http://192.0.2.1:43127",
		"https://127.0.0.1:43127",
	} {
		if err := ValidateE2EControllerURL(value); err == nil {
			t.Errorf("unsafe E2E Controller URL %q unexpectedly accepted", value)
		}
	}
}
