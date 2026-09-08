//go:build windows

package runtime

import (
	"os/exec"
	"syscall"
)

const (
	detachedProcess = 0x00000008
)

// configureRuntimeProcess keeps the console-subsystem Runtime child detached
// from the desktop when the Supervisor is launched by the GUI-subsystem task
// host. DETACHED_PROCESS is intentionally the only creation flag: it prevents
// a console-subsystem child from inheriting/creating a desktop console in this
// GUI-host path. HideWindow remains a harmless STARTF_USESHOWWINDOW/SW_HIDE
// fallback. Stdin/stdout/stderr remain explicit pipes, so the Supervisor READY
// and graceful STOP contracts are unchanged.
func configureRuntimeProcess(command *exec.Cmd) {
	if command == nil {
		return
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess,
		HideWindow:    true,
	}
}
