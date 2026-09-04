//go:build windows

package installedlifecycle

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestWindowsTaskSchedulerExactTaskRoundTrip(t *testing.T) {
	t.Setenv(E2EModeEnv, "1")
	taskName := `\ProxyLens-Test\` + uuid.NewString()
	t.Setenv(E2ETaskNameEnv, taskName)
	t.Setenv(E2ETaskExecutableEnv, `C:\Windows\System32\cmd.exe`)

	if err := Register(taskName, `C:\Windows\System32\cmd.exe`); err != nil {
		t.Fatalf("isolated test task registration failed: %v", err)
	}
	defer func() {
		if err := Unregister(taskName); err != nil {
			t.Errorf("isolated test task cleanup failed: %v", err)
		}
	}()
	status, err := Status(taskName)
	if err != nil {
		t.Fatalf("isolated task status failed: %v", err)
	}
	if !status.Registered || !status.Enabled {
		t.Fatalf("isolated task status=%+v, want registered and enabled", status)
	}
	if status.ActionPath != filepath.Clean(`C:\Windows\System32\cmd.exe`) {
		t.Fatalf("isolated task action=%q, want harmless cmd.exe", status.ActionPath)
	}
	if err := Unregister(taskName); err != nil {
		t.Fatalf("isolated task unregister failed: %v", err)
	}
	status, err = Status(taskName)
	if err != nil {
		t.Fatalf("isolated task post-unregister status failed: %v", err)
	}
	if status.Registered {
		t.Fatalf("isolated task remained after exact unregister: %+v", status)
	}
}
