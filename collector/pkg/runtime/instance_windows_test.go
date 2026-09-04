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
