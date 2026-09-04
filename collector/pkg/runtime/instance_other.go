//go:build !windows

package runtime

import "fmt"

func acquireNamedOwnership(instanceKey string, alreadyRunningErr error) (*RuntimeOwnership, error) {
	return nil, fmt.Errorf("%w: %s", ErrRuntimeOwnershipUnsupported, instanceKey)
}

func probeNamedOwnership(instanceKey string) (bool, error) {
	return false, fmt.Errorf("%w: %s", ErrRuntimeOwnershipUnsupported, instanceKey)
}

func openSupervisorStopEvent(eventName string) (*SupervisorStopEvent, error) {
	return nil, fmt.Errorf("%w: %s", ErrRuntimeOwnershipUnsupported, eventName)
}

func signalSupervisorStopEvent(eventName string) error {
	return fmt.Errorf("%w: %s", ErrRuntimeOwnershipUnsupported, eventName)
}
