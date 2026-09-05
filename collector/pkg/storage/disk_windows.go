//go:build windows

package storage

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var procGetDiskFreeSpaceExW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// nearestExistingDir finds the nearest existing directory ancestor for path.
// It NEVER creates directories. If all ancestors do not exist or path is relative,
// it falls back to the volume root (e.g. C:\) or clean parent.
func nearestExistingDir(dir string) string {
	cleaned := filepath.Clean(dir)
	target := cleaned
	for {
		if fi, err := os.Stat(target); err == nil && fi.IsDir() {
			return target
		}
		parent := filepath.Dir(target)
		if parent == target {
			break
		}
		target = parent
	}
	if vol := filepath.VolumeName(cleaned); vol != "" {
		volRoot := vol + string(filepath.Separator)
		if fi, err := os.Stat(volRoot); err == nil && fi.IsDir() {
			return volRoot
		}
		return volRoot
	}
	return target
}

// freeDiskBytes reports the free bytes on the volume holding dir. ok is false
// when the platform query fails and callers must treat the requirement as
// unknown rather than failed.
func freeDiskBytes(dir string) (uint64, bool) {
	probeDir := nearestExistingDir(dir)
	p, err := syscall.UTF16PtrFromString(probeDir)
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
