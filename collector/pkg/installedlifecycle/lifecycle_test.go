package installedlifecycle

import (
	"os"
	"path/filepath"
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

func TestTaskScheduleOverrideIsE2EOnly(t *testing.T) {
	t.Setenv(E2EModeEnv, "")
	t.Setenv(E2ETaskScheduleEnv, "1")
	if isE2ETaskScheduleEnabled() {
		t.Fatal("E2E task activation trigger was enabled outside E2E mode")
	}

	t.Setenv(E2EModeEnv, "1")
	t.Setenv(E2ETaskScheduleEnv, "")
	if isE2ETaskScheduleEnabled() {
		t.Fatal("E2E task activation trigger was enabled without its explicit opt-in")
	}
	t.Setenv(E2ETaskScheduleEnv, "1")
	if !isE2ETaskScheduleEnabled() {
		t.Fatal("explicit E2E task activation trigger was not enabled")
	}
}
