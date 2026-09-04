//go:build windows

package runtimeconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric           = 1
	credPersistLocalMachine   = 2
	credMaxCredentialBlobSize = 512
)

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

type credentialAttribute struct {
	KeywordName *uint16
	Flags       uint32
	ValueSize   uint32
	Value       *byte
}

// credential mirrors the Windows CREDENTIALW layout. The wrapper intentionally
// exposes only the generic blob and never returns the native structure.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         *credentialAttribute
	TargetAlias        *uint16
	UserName           *uint16
}

type windowsCredentialStore struct {
	target string
}

func newCredentialStore(target string) (SecretStore, error) {
	return &windowsCredentialStore{target: target}, nil
}

func (s *windowsCredentialStore) Read(ctx context.Context) (string, bool, error) {
	if err := contextError(ctx); err != nil {
		return "", false, err
	}
	target, err := windows.UTF16PtrFromString(s.target)
	if err != nil {
		return "", false, fmt.Errorf("invalid Credential Manager target")
	}
	var native *credential
	result, _, callErr := procCredRead.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(credTypeGeneric),
		0,
		uintptr(unsafe.Pointer(&native)),
	)
	if result == 0 {
		if errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("CredReadW failed: %w", callErr)
	}
	if native == nil {
		return "", false, fmt.Errorf("CredReadW returned an empty credential")
	}
	defer func() { _, _, _ = procCredFree.Call(uintptr(unsafe.Pointer(native))) }()
	if native.CredentialBlobSize == 0 || native.CredentialBlob == nil {
		return "", true, nil
	}
	blob := unsafe.Slice(native.CredentialBlob, int(native.CredentialBlobSize))
	return string(append([]byte(nil), blob...)), true, nil
}

func (s *windowsCredentialStore) Write(ctx context.Context, value string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("Controller Secret must not be blank")
	}
	blob := []byte(value)
	if len(blob) > credMaxCredentialBlobSize {
		return fmt.Errorf("Controller Secret exceeds the Windows Credential Manager generic blob limit")
	}
	target, err := windows.UTF16PtrFromString(s.target)
	if err != nil {
		return fmt.Errorf("invalid Credential Manager target")
	}
	native := credential{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
	}
	result, _, callErr := procCredWrite.Call(uintptr(unsafe.Pointer(&native)), 0)
	if result == 0 {
		return fmt.Errorf("CredWriteW failed: %w", callErr)
	}
	return nil
}

func (s *windowsCredentialStore) Delete(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(s.target)
	if err != nil {
		return fmt.Errorf("invalid Credential Manager target")
	}
	result, _, callErr := procCredDelete.Call(uintptr(unsafe.Pointer(target)), uintptr(credTypeGeneric), 0)
	if result == 0 && !errors.Is(callErr, windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("CredDeleteW failed: %w", callErr)
	}
	return nil
}
