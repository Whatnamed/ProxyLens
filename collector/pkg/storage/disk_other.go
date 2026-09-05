//go:build !windows

package storage

// freeDiskBytes is a no-op on non-Windows platforms: the seed preflight
// treats the requirement as unknown and does not block.
func freeDiskBytes(dir string) (uint64, bool) {
	return 0, false
}
