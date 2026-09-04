//go:build !windows

package runtime

import "fmt"

func acquireNamedOwnership(instanceKey string, alreadyRunningErr error) (*RuntimeOwnership, error) {
	return nil, fmt.Errorf("%w: %s", ErrRuntimeOwnershipUnsupported, instanceKey)
}

func probeNamedOwnership(instanceKey string) (bool, error) {
	return false, fmt.Errorf("%w: %s", ErrRuntimeOwnershipUnsupported, instanceKey)
}
