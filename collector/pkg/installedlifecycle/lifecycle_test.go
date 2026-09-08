package installedlifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveTaskIdentityIsProductionOnlyOutsideE2E(t *testing.T) {
	t.Setenv(E2EModeEnv, "")
	t.Setenv(E2ETaskNameEnv, "")
	name, err := ResolveTaskName()
	if err != nil || name != ProductionTaskName {
		t.Fatalf("production task identity=%q err=%v", name, err)
	}
	t.Setenv(E2ETaskNameEnv, `\ProxyLens-Test\00000000-0000-4000-8000-000000000000`)
	if _, err := ResolveTaskName(); err == nil {
		t.Fatal("test task override was accepted outside E2E")
	}
}

func TestResolveTaskIdentityRequiresRandomE2ETask(t *testing.T) {
	t.Setenv(E2EModeEnv, "1")
	for _, value := range []string{
		"",
		ProductionTaskName,
		`\ProxyLens-Test\not-a-uuid`,
		`\Other\00000000-0000-4000-8000-000000000000`,
	} {
		t.Setenv(E2ETaskNameEnv, value)
		if _, err := ResolveTaskName(); err == nil {
			t.Fatalf("unsafe E2E task identity %q was accepted", value)
		}
	}
	t.Setenv(E2ETaskNameEnv, `\ProxyLens-Test\00000000-0000-4000-8000-000000000000`)
	name, err := ResolveTaskName()
	if err != nil || name == "" {
		t.Fatalf("valid E2E task identity=%q err=%v", name, err)
	}
}

func TestResolveTaskExecutableOverrideIsE2EOnly(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "harmless-fixture.exe")
	if err := os.WriteFile(executable, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(E2ETaskExecutableEnv, executable)
	t.Setenv(E2EModeEnv, "")
	if _, err := ResolveTaskExecutable(executable); err == nil {
		t.Fatal("task executable override was accepted outside E2E")
	}
	t.Setenv(E2EModeEnv, "1")
	resolved, err := ResolveTaskExecutable(filepath.Join(t.TempDir(), "other.exe"))
	if err != nil || resolved != executable {
		t.Fatalf("E2E task executable=%q err=%v, want %q", resolved, err, executable)
	}
}

func TestResolveTaskArgumentsOverrideIsE2EOnly(t *testing.T) {
	t.Setenv(E2ETaskArgumentsEnv, `/D /S /C "wrapper.cmd"`)
	if _, err := ResolveTaskArguments(); err == nil {
		t.Fatal("non-E2E task argument override was accepted")
	}
	t.Setenv(E2EModeEnv, "1")
	arguments, err := ResolveTaskArguments()
	if err != nil || arguments == "" {
		t.Fatalf("E2E task arguments were rejected: %q, %v", arguments, err)
	}
}

func TestInstalledLayoutRequiresAcceptedSiblingSetAndUninstaller(t *testing.T) {
	dir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	for _, name := range []string{
		"proxylens-supervisor" + ext,
		"proxylens-supervisor-host" + ext,
		"proxylens-runtime" + ext,
		"proxylens-query-api" + ext,
		"proxylens-desktop" + ext,
		"uninstall" + ext,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	supervisor := filepath.Join(dir, "proxylens-supervisor"+ext)
	if !IsInstalledLayout(supervisor) {
		t.Fatal("accepted isolated installed layout was not detected")
	}
	if err := os.Remove(filepath.Join(dir, "uninstall"+ext)); err != nil {
		t.Fatal(err)
	}
	if IsInstalledLayout(supervisor) {
		t.Fatal("layout without uninstall marker was accepted")
	}
}

func TestDeveloperDirectoryIsNotInstalledLayout(t *testing.T) {
	dir := t.TempDir()
	supervisor := filepath.Join(dir, "proxylens-supervisor.exe")
	if err := os.WriteFile(supervisor, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if IsInstalledLayout(supervisor) {
		t.Fatal("random developer directory was accepted as installed")
	}
}
