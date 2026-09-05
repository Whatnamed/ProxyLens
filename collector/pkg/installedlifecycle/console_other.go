//go:build !windows

package installedlifecycle

// HideOwnedConsoleWindow is a no-op on platforms without a Windows console.
func HideOwnedConsoleWindow() {}
