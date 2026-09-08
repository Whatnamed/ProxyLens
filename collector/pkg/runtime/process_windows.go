//go:build windows

package runtime

import (
	"os/exec"
	"syscall"
)

const (
	createNoWindow  = 0x08000000
	detachedProcess = 0x00000008
)

// configureRuntimeProcess keeps the console-subsystem Runtime child detached
// from the desktop when the Supervisor is launched by the GUI-subsystem task
// host. Stdin/stdout/stderr remain explicit pipes, so the Supervisor READY and
// graceful STOP contracts are unchanged.
func configureRuntimeProcess(command *exec.Cmd) {
	if command == nil {
		return
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow | detachedProcess,
		HideWindow:    true,
	}
}
