//go:build windows

package runtime

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

func acquireNamedOwnership(instanceKey string, alreadyRunningErr error) (*RuntimeOwnership, error) {
	acquired := make(chan mutexAcquireResult, 1)
	release := make(chan chan error)

	// Windows mutex ownership is tied to an OS thread, while a Go goroutine is
	// free to migrate between threads. Keep the native handle and its release
	// operation on one locked thread for the complete Runtime lifetime.
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		handle, err := createAndAcquireMutex(instanceKey, alreadyRunningErr)
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

func probeNamedOwnership(instanceKey string) (bool, error) {
	resultCh := make(chan mutexProbeResult, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		handle, err := createMutexHandle(instanceKey)
		if err != nil {
			resultCh <- mutexProbeResult{err: err}
			return
		}
		waitResult, waitErr := windows.WaitForSingleObject(handle, 0)
		switch waitResult {
		case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
			releaseErr := windows.ReleaseMutex(handle)
			closeErr := windows.CloseHandle(handle)
			if waitErr != nil {
				resultCh <- mutexProbeResult{err: fmt.Errorf("failed to probe runtime ownership mutex: %w", waitErr)}
				return
			}
			if releaseErr != nil {
				resultCh <- mutexProbeResult{err: fmt.Errorf("failed to release runtime ownership probe: %w", releaseErr)}
				return
			}
			resultCh <- mutexProbeResult{closeErr: closeErr}
		case uint32(windows.WAIT_TIMEOUT):
			resultCh <- mutexProbeResult{present: true, closeErr: windows.CloseHandle(handle)}
		default:
			_ = windows.CloseHandle(handle)
			if waitErr != nil {
				resultCh <- mutexProbeResult{err: fmt.Errorf("failed to probe runtime ownership mutex: %w", waitErr)}
			} else {
				resultCh <- mutexProbeResult{err: fmt.Errorf("failed to probe runtime ownership mutex: result=%d", waitResult)}
			}
		}
	}()
	result := <-resultCh
	if result.err != nil {
		return false, result.err
	}
	if result.closeErr != nil {
		return false, fmt.Errorf("failed to close runtime ownership probe: %w", result.closeErr)
	}
	return result.present, nil
}

type mutexAcquireResult struct {
	handle windows.Handle
	err    error
}

type mutexProbeResult struct {
	present  bool
	err      error
	closeErr error
}

func createAndAcquireMutex(instanceKey string, alreadyRunningErr error) (windows.Handle, error) {
	handle, err := createMutexHandle(instanceKey)
	if err != nil {
		return 0, err
	}

	waitResult, waitErr := windows.WaitForSingleObject(handle, 0)
	switch waitResult {
	case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
		if waitErr != nil {
			_ = windows.CloseHandle(handle)
			return 0, fmt.Errorf("failed to acquire ownership mutex: %w", waitErr)
		}
		return handle, nil
	case uint32(windows.WAIT_TIMEOUT):
		_ = windows.CloseHandle(handle)
		return 0, fmt.Errorf("%w: %s", alreadyRunningErr, instanceKey)
	default:
		_ = windows.CloseHandle(handle)
		if waitErr != nil {
			return 0, fmt.Errorf("failed to wait for ownership mutex: %w", waitErr)
		}
		return 0, fmt.Errorf("failed to wait for ownership mutex: result=%d", waitResult)
	}
}

func createMutexHandle(instanceKey string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(instanceKey)
	if err != nil {
		return 0, fmt.Errorf("failed to encode runtime ownership name: %w", err)
	}

	// CreateMutex returns a usable handle and ERROR_ALREADY_EXISTS when the
	// named object already exists. The caller performs a zero-timeout wait so
	// ownership and non-destructive probing share the same native primitive.
	handle, createErr := windows.CreateMutex(nil, false, name)
	if handle == 0 {
		if createErr != nil {
			return 0, fmt.Errorf("failed to create runtime ownership mutex: %w", createErr)
		}
		return 0, fmt.Errorf("failed to create runtime ownership mutex: empty handle")
	}
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		return 0, fmt.Errorf("failed to create ownership mutex: %w", createErr)
	}
	return handle, nil
}

func openSupervisorStopEvent(eventName string) (*SupervisorStopEvent, error) {
	name, err := windows.UTF16PtrFromString(eventName)
	if err != nil {
		return nil, fmt.Errorf("failed to encode Supervisor stop event name: %w", err)
	}
	handle, createErr := windows.CreateEvent(nil, 1, 0, name)
	if handle == 0 {
		if createErr != nil {
			return nil, fmt.Errorf("failed to create Supervisor stop event: %w", createErr)
		}
		return nil, fmt.Errorf("failed to create Supervisor stop event: empty handle")
	}
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("failed to create Supervisor stop event: %w", createErr)
	}
	if err := windows.ResetEvent(handle); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("failed to reset Supervisor stop event: %w", err)
	}
	return &SupervisorStopEvent{
		closeFunc: func() error { return windows.CloseHandle(handle) },
		waitFunc: func(ctx context.Context, cancel func()) error {
			if ctx == nil {
				ctx = context.Background()
			}
			for {
				select {
				case <-ctx.Done():
					return nil
				default:
				}
				result, waitErr := windows.WaitForSingleObject(handle, 100)
				if waitErr != nil {
					return fmt.Errorf("failed to wait for Supervisor stop event: %w", waitErr)
				}
				switch result {
				case windows.WAIT_OBJECT_0:
					if cancel != nil {
						cancel()
					}
					return nil
				case uint32(windows.WAIT_TIMEOUT):
				default:
					return fmt.Errorf("failed to wait for Supervisor stop event: result=%d", result)
				}
			}
		},
	}, nil
}

func signalSupervisorStopEvent(eventName string) error {
	name, err := windows.UTF16PtrFromString(eventName)
	if err != nil {
		return fmt.Errorf("failed to encode Supervisor stop event name: %w", err)
	}
	handle, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return fmt.Errorf("failed to open Supervisor stop event: %w", err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.SetEvent(handle); err != nil {
		return fmt.Errorf("failed to signal Supervisor stop event: %w", err)
	}
	return nil
}
