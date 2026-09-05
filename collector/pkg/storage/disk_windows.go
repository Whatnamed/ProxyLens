//go:build windows

package storage

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

var procGetDiskFreeSpaceExW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// freeDiskBytes reports the free bytes on the volume holding dir. ok is false
// when the platform query fails and callers must treat the requirement as
// unknown rather than failed.
func freeDiskBytes(dir string) (uint64, bool) {
	p, err := syscall.UTF16PtrFromString(filepath.Clean(dir))
	if err != nil {
		return 0, false
	}
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	r1, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r1 == 0 {
		return 0, false
	}
	return totalFreeBytes, true
}
