//go:build windows

package runtime

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestWindowsRuntimeOwnershipIsPathKeyedAndReleasable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "authority.db")
	first, err := AcquireRuntimeOwnership(dbPath)
	if err != nil {
		t.Fatalf("first ownership acquisition failed: %v", err)
	}
	defer first.Close()

	second, err := AcquireRuntimeOwnership(dbPath)
	if second != nil {
		_ = second.Close()
		t.Fatal("second ownership acquisition unexpectedly returned a handle")
	}
	if !errors.Is(err, ErrRuntimeAlreadyRunning) {
		t.Fatalf("expected typed already-running error, got %v", err)
	}

	other, err := AcquireRuntimeOwnership(filepath.Join(dir, "other.db"))
	if err != nil {
		t.Fatalf("different database should acquire independently: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Fatalf("different database ownership close failed: %v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("first ownership close failed: %v", err)
	}
	reacquired, err := AcquireRuntimeOwnership(dbPath)
	if err != nil {
		t.Fatalf("ownership was not released for reacquisition: %v", err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatalf("reacquired ownership close failed: %v", err)
	}
}

func TestWindowsRuntimePresenceProbeDoesNotRetainOwnership(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "authority.db")
	presence, err := ProbeRuntimePresence(dbPath)
	if err != nil {
		t.Fatalf("initial Runtime presence probe failed: %v", err)
	}
	if presence != RuntimeAbsent {
		t.Fatalf("initial Runtime presence = %q, want absent", presence)
	}

	ownership, err := AcquireRuntimeOwnership(dbPath)
	if err != nil {
		t.Fatalf("Runtime ownership acquisition failed: %v", err)
	}
	presence, err = ProbeRuntimePresence(dbPath)
	if err != nil {
		t.Fatalf("held Runtime presence probe failed: %v", err)
	}
	if presence != RuntimePresent {
		t.Fatalf("held Runtime presence = %q, want present", presence)
	}
	if err := ownership.Close(); err != nil {
		t.Fatalf("Runtime ownership close failed: %v", err)
	}

	presence, err = ProbeRuntimePresence(dbPath)
	if err != nil {
		t.Fatalf("post-release Runtime presence probe failed: %v", err)
	}
	if presence != RuntimeAbsent {
		t.Fatalf("post-release Runtime presence = %q, want absent", presence)
	}
	reacquired, err := AcquireRuntimeOwnership(dbPath)
	if err != nil {
		t.Fatalf("probe retained the Runtime mutex: %v", err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatalf("reacquired Runtime ownership close failed: %v", err)
	}
}

func TestWindowsSupervisorOwnershipIsIndependentAndPathKeyed(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "authority.db")
	first, err := AcquireSupervisorOwnership(dbPath)
	if err != nil {
		t.Fatalf("first Supervisor ownership acquisition failed: %v", err)
	}
	second, err := AcquireSupervisorOwnership(dbPath)
	if second != nil {
		_ = second.Close()
		t.Fatal("duplicate Supervisor unexpectedly returned an ownership handle")
	}
	if !errors.Is(err, ErrSupervisorAlreadyRunning) {
		t.Fatalf("expected typed duplicate Supervisor error, got %v", err)
	}
	runtimeOwner, err := AcquireRuntimeOwnership(dbPath)
	if err != nil {
		t.Fatalf("Runtime and Supervisor should coexist for one DB: %v", err)
	}
	otherSupervisor, err := AcquireSupervisorOwnership(filepath.Join(dir, "other.db"))
	if err != nil {
		t.Fatalf("different DB Supervisor should acquire independently: %v", err)
	}
	if err := otherSupervisor.Close(); err != nil {
		t.Fatalf("different DB Supervisor close failed: %v", err)
	}
	if err := runtimeOwner.Close(); err != nil {
		t.Fatalf("Runtime ownership close failed: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Supervisor ownership close failed: %v", err)
	}
}
