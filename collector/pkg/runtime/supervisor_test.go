package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestartBackoffIsBoundedAndResettable(t *testing.T) {
	backoff := NewRestartBackoff(RestartPolicy{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     40 * time.Millisecond,
		StableAfter:  time.Second,
	})
	for index, want := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond} {
		if got := backoff.NextDelay(); got != want {
			t.Fatalf("delay %d = %s, want %s", index, got, want)
		}
	}
	backoff.Reset()
	if got := backoff.NextDelay(); got != 10*time.Millisecond {
		t.Fatalf("reset delay = %s, want 10ms", got)
	}
}

func TestResolveRuntimeExecutableUsesExplicitPrecedenceAndSiblingSuffix(t *testing.T) {
	dir := t.TempDir()
	suffix := "-x86_64-pc-windows-msvc.exe"
	supervisor := filepath.Join(dir, "proxylens-supervisor"+suffix)
	runtime := filepath.Join(dir, "proxylens-runtime"+suffix)
	cli := filepath.Join(dir, "explicit-runtime.exe")
	envRuntime := filepath.Join(dir, "env-runtime.exe")
	for _, path := range []string{supervisor, runtime, cli, envRuntime} {
		if err := os.WriteFile(path, []byte("test executable placeholder"), 0o600); err != nil {
			t.Fatalf("failed to create %s: %v", path, err)
		}
	}

	got, err := ResolveRuntimeExecutable("", "", supervisor)
	if err != nil {
		t.Fatalf("sibling Runtime resolution failed: %v", err)
	}
	if got != runtime {
		t.Fatalf("sibling Runtime = %q, want %q", got, runtime)
	}
	got, err = ResolveRuntimeExecutable(cli, envRuntime, supervisor)
	if err != nil || got != cli {
		t.Fatalf("CLI Runtime precedence got %q, err=%v; want %q", got, err, cli)
	}
	got, err = ResolveRuntimeExecutable("", envRuntime, supervisor)
	if err != nil || got != envRuntime {
		t.Fatalf("environment Runtime precedence got %q, err=%v; want %q", got, err, envRuntime)
	}
}

func TestEnvironmentWithControllerOverrideReplacesOnlyControllerVariable(t *testing.T) {
	result := environmentWithControllerOverride([]string{
		"PATH=fixture",
		ControllerURLEnv + "=old",
		ControllerURLEnv + "=duplicate",
	}, "http://127.0.0.1:43127")
	count := 0
	for _, entry := range result {
		if entry == ControllerURLEnv+"=http://127.0.0.1:43127" {
			count++
		}
		if entry == ControllerURLEnv+"=old" || entry == ControllerURLEnv+"=duplicate" {
			t.Fatalf("old Controller override survived: %q", entry)
		}
	}
	if count != 1 {
		t.Fatalf("Controller override count = %d, want 1", count)
	}
}

func TestNewSupervisorRequiresRegularRuntimeExecutable(t *testing.T) {
	_, err := NewSupervisor(SupervisorOptions{
		DBPath:     filepath.Join(t.TempDir(), "authority.db"),
		RuntimeExe: filepath.Join(t.TempDir(), "missing-runtime.exe"),
	})
	if err == nil {
		t.Fatal("Supervisor accepted a missing Runtime executable")
	}
}
