package runtimeconfig

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

type SecretStore interface {
	Read(ctx context.Context) (value string, found bool, err error)
	Write(ctx context.Context, value string) error
	Delete(ctx context.Context) error
}

// ResolveCredentialTarget never permits an E2E override to select the
// production target. Outside E2E the test-only environment variable is
// ignored and the product target is fixed.
func ResolveCredentialTarget(e2eMode bool, override string) (string, error) {
	if !e2eMode {
		return ProductionCredentialTarget, nil
	}
	value := strings.TrimSpace(override)
	if value == "" {
		return "", nil
	}
	if err := ValidateTestCredentialTarget(value); err != nil {
		return "", err
	}
	return value, nil
}

func NewConfiguredSecretStore(e2eMode bool) (SecretStore, string, error) {
	target, err := ResolveCredentialTarget(e2eMode, os.Getenv(E2ECredentialTargetEnv))
	if err != nil {
		return nil, "", err
	}
	if target == "" {
		return nil, "", nil
	}
	store, err := NewCredentialStoreForTarget(target)
	if errors.Is(err, ErrCredentialStoreUnsupported) {
		return nil, target, nil
	}
	if err != nil {
		return nil, "", err
	}
	return store, target, nil
}

func NewCredentialStoreForTarget(target string) (SecretStore, error) {
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("Credential Manager target must not be blank")
	}
	return newCredentialStore(target)
}

func ValidateTestCredentialTarget(target string) error {
	if !strings.HasPrefix(target, TestCredentialTargetPrefix) {
		return fmt.Errorf("E2E Credential Manager target must begin with %s", TestCredentialTargetPrefix)
	}
	suffix := strings.TrimPrefix(target, TestCredentialTargetPrefix)
	if len(suffix) != 36 || suffix[8] != '-' || suffix[13] != '-' || suffix[18] != '-' || suffix[23] != '-' {
		return fmt.Errorf("E2E Credential Manager target must end with a UUID")
	}
	for index, character := range suffix {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return fmt.Errorf("E2E Credential Manager target must end with a UUID")
		}
	}
	return nil
}

func NewTestCredentialTarget() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("failed to generate test Credential Manager target: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	uuid := encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
	return TestCredentialTargetPrefix + uuid, nil
}

type MemorySecretStore struct {
	mu    sync.Mutex
	value string
	found bool
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{}
}

func (s *MemorySecretStore) Read(ctx context.Context) (string, bool, error) {
	if err := contextError(ctx); err != nil {
		return "", false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.found, nil
}

func (s *MemorySecretStore) Write(ctx context.Context, value string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("Controller Secret must not be blank")
	}
	s.mu.Lock()
	s.value = value
	s.found = true
	s.mu.Unlock()
	return nil
}

func (s *MemorySecretStore) Delete(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.value = ""
	s.found = false
	s.mu.Unlock()
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
