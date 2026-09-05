//go:build windows

package installedlifecycle

import (
	"syscall"
	"unsafe"
)

const (
	swHideConstant = 0x0000
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWin   = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcL = kernel32.NewProc("GetConsoleProcessList")
	user32              = syscall.NewLazyDLL("user32.dll")
	procShowWindow      = user32.NewProc("ShowWindow")
)

// HideOwnedConsoleWindow hides the dedicated console window Windows allocates
// when an interactive Task Scheduler owner launches this console executable on
// the user's desktop. A console shared with an interactive shell (developer
// runs) stays visible because other processes are attached to it.
func HideOwnedConsoleWindow() {
	window, _, _ := procGetConsoleWin.Call()
	if window == 0 {
		return
	}
	var processes [8]uint32
	count, _, _ := procGetConsoleProcL.Call(
		uintptr(unsafe.Pointer(&processes[0])),
		uintptr(len(processes)),
	)
	if count != 1 {
		return
	}
	_, _, _ = procShowWindow.Call(window, uintptr(swHideConstant))
}
