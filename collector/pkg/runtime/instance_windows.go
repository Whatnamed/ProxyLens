//go:build windows

package runtime

import (
	"errors"
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

func acquireRuntimeOwnership(instanceKey string) (*RuntimeOwnership, error) {
	acquired := make(chan mutexAcquireResult, 1)
	release := make(chan chan error)

	// Windows mutex ownership is tied to an OS thread, while a Go goroutine is
	// free to migrate between threads. Keep the native handle and its release
	// operation on one locked thread for the complete Runtime lifetime.
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		handle, err := createAndAcquireMutex(instanceKey)
		acquired <- mutexAcquireResult{handle: handle, err: err}
		if err != nil {
			return
		}

		releaseResult := <-release
		releaseErr := windows.ReleaseMutex(handle)
		closeErr := windows.CloseHandle(handle)
		if releaseErr != nil {
			releaseResult <- fmt.Errorf("failed to release runtime ownership mutex: %w", releaseErr)
		} else {
			releaseResult <- closeErr
		}
	}()

	result := <-acquired
	if result.err != nil {
		return nil, result.err
	}
	return newRuntimeOwnership(func() error {
		releaseResult := make(chan error, 1)
		release <- releaseResult
		return <-releaseResult
	}), nil
}

type mutexAcquireResult struct {
	handle windows.Handle
	err    error
}

func createAndAcquireMutex(instanceKey string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(instanceKey)
	if err != nil {
		return 0, fmt.Errorf("failed to encode runtime ownership name: %w", err)
	}

	// CreateMutex returns a usable handle and ERROR_ALREADY_EXISTS when the
	// named object already exists. Waiting with a zero timeout makes both the
	// new-object and existing-but-unowned cases explicit and race-safe.
	handle, createErr := windows.CreateMutex(nil, false, name)
	if handle == 0 {
		if createErr != nil {
			return 0, fmt.Errorf("failed to create runtime ownership mutex: %w", createErr)
		}
		return 0, fmt.Errorf("failed to create runtime ownership mutex: empty handle")
	}
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		return 0, fmt.Errorf("failed to create runtime ownership mutex: %w", createErr)
	}

	waitResult, waitErr := windows.WaitForSingleObject(handle, 0)
	switch waitResult {
	case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
		if waitErr != nil {
			_ = windows.CloseHandle(handle)
			return 0, fmt.Errorf("failed to acquire runtime ownership mutex: %w", waitErr)
		}
		return handle, nil
	case uint32(windows.WAIT_TIMEOUT):
		_ = windows.CloseHandle(handle)
		return 0, fmt.Errorf("%w: %s", ErrRuntimeAlreadyRunning, instanceKey)
	default:
		_ = windows.CloseHandle(handle)
		if waitErr != nil {
			return 0, fmt.Errorf("failed to wait for runtime ownership mutex: %w", waitErr)
		}
		return 0, fmt.Errorf("failed to wait for runtime ownership mutex: result=%d", waitResult)
	}
}
