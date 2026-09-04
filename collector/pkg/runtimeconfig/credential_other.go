//go:build !windows

package runtimeconfig

import "fmt"

func newCredentialStore(target string) (SecretStore, error) {
	return nil, fmt.Errorf("%w: target %s", ErrCredentialStoreUnsupported, target)
}
