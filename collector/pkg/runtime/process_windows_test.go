//go:build windows

package runtime

import (
	"os/exec"
	"testing"
)

func TestConfigureRuntimeProcessUsesDetachedProcessWithoutCreateNoWindow(t *testing.T) {
	command := exec.Command("proxylens-runtime.exe")
	configureRuntimeProcess(command)
	if command.SysProcAttr == nil {
		t.Fatal("Runtime process configuration did not set SysProcAttr")
	}
	if command.SysProcAttr.CreationFlags != detachedProcess {
		t.Fatalf("Runtime creation flags = %#x, want DETACHED_PROCESS %#x only", command.SysProcAttr.CreationFlags, detachedProcess)
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("Runtime process configuration should retain HideWindow as a harmless fallback")
	}
}
