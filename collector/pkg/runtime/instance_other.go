//go:build !windows

package runtime

import "fmt"

func acquireRuntimeOwnership(instanceKey string) (*RuntimeOwnership, error) {
	return nil, fmt.Errorf("%w: %s", ErrRuntimeOwnershipUnsupported, instanceKey)
}
