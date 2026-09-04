package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

var (
	// ErrRuntimeAlreadyRunning means another Runtime owns the same authority
	// database identity. Callers may handle this as a clean, non-fatal result.
	ErrRuntimeAlreadyRunning = errors.New("ProxyLens runtime already running")

	// ErrRuntimeOwnershipUnsupported is returned on platforms where the
	// Windows named-mutex ownership contract is not available.
	ErrRuntimeOwnershipUnsupported = errors.New("ProxyLens runtime ownership is unsupported on this platform")

	// ErrSupervisorAlreadyRunning means another Supervisor owns the same
	// authority database identity.
	ErrSupervisorAlreadyRunning = errors.New("ProxyLens supervisor already running")
)

// RuntimeOwnership is an idempotent handle to a per-database process
// ownership lease. The underlying platform handle is held until Close.
type RuntimeOwnership struct {
	closeOnce sync.Once
	closeFunc func() error
	closeErr  error
}

var localOwnership = struct {
	sync.Mutex
	keys map[string]struct{}
}{keys: make(map[string]struct{})}

func newRuntimeOwnership(closeFunc func() error) *RuntimeOwnership {
	return &RuntimeOwnership{closeFunc: closeFunc}
}

// Close releases the ownership lease once. Repeated calls return the same
// result and never attempt to release the platform handle twice.
func (o *RuntimeOwnership) Close() error {
	if o == nil {
		return nil
	}
	o.closeOnce.Do(func() {
		if o.closeFunc != nil {
			o.closeErr = o.closeFunc()
		}
	})
	return o.closeErr
}

// RuntimeInstanceKey returns the deterministic, path-keyed Windows object
// name for an authority database. The raw path is normalized for Windows'
// case-insensitive filesystem semantics and then hashed so it never appears
// in the named object identity.
func RuntimeInstanceKey(dbPath string) (string, error) {
	return instanceKey(dbPath, `Local\ProxyLens.Runtime.v1.`)
}

// SupervisorInstanceKey returns the deterministic per-database Supervisor
// object name. The raw path is hashed and never appears in the name.
func SupervisorInstanceKey(dbPath string) (string, error) {
	return instanceKey(dbPath, `Local\ProxyLens.Supervisor.v1.`)
}

func instanceKey(dbPath, prefix string) (string, error) {
	if strings.TrimSpace(dbPath) == "" {
		return "", fmt.Errorf("runtime ownership requires a database path")
	}

	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to normalize runtime database path: %w", err)
	}
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(absPath)))
	digest := sha256.Sum256([]byte(normalized))
	return prefix + hex.EncodeToString(digest[:]), nil
}

// AcquireRuntimeOwnership acquires one OS-level writer lease for dbPath.
// The platform implementation owns the exact handle for the caller's full
// Runtime lifetime; it is not a PID-file approximation.
func AcquireRuntimeOwnership(dbPath string) (*RuntimeOwnership, error) {
	return acquireInstanceOwnership(dbPath, `Local\ProxyLens.Runtime.v1.`, ErrRuntimeAlreadyRunning)
}

// AcquireSupervisorOwnership acquires the independent per-database
// Supervisor lease. It intentionally does not share the Runtime mutex name.
func AcquireSupervisorOwnership(dbPath string) (*RuntimeOwnership, error) {
	return acquireInstanceOwnership(dbPath, `Local\ProxyLens.Supervisor.v1.`, ErrSupervisorAlreadyRunning)
}

func acquireInstanceOwnership(dbPath, prefix string, alreadyRunningErr error) (*RuntimeOwnership, error) {
	key, err := instanceKey(dbPath, prefix)
	if err != nil {
		return nil, err
	}

	// Windows mutexes are recursive for the owning thread. Keep a small
	// process-local guard as well so two library callers in one Runtime process
	// cannot accidentally acquire the same authority twice.
	localOwnership.Lock()
	defer localOwnership.Unlock()
	if _, exists := localOwnership.keys[key]; exists {
		return nil, fmt.Errorf("%w: %s", alreadyRunningErr, key)
	}

	ownership, err := acquireNamedOwnership(key, alreadyRunningErr)
	if err != nil {
		return nil, err
	}
	localOwnership.keys[key] = struct{}{}
	originalClose := ownership.closeFunc
	ownership.closeFunc = func() error {
		closeErr := originalClose()
		localOwnership.Lock()
		delete(localOwnership.keys, key)
		localOwnership.Unlock()
		return closeErr
	}
	return ownership, nil
}

type RuntimePresence string

const (
	RuntimePresent RuntimePresence = "present"
	RuntimeAbsent  RuntimePresence = "absent"
)

// ProbeRuntimePresence checks the Runtime mutex without retaining ownership.
// A successful temporary acquisition means no Runtime currently owns the DB;
// the platform implementation releases that temporary lease before returning.
func ProbeRuntimePresence(dbPath string) (RuntimePresence, error) {
	key, err := RuntimeInstanceKey(dbPath)
	if err != nil {
		return "", err
	}
	localOwnership.Lock()
	_, localPresent := localOwnership.keys[key]
	localOwnership.Unlock()
	if localPresent {
		return RuntimePresent, nil
	}
	present, err := probeNamedOwnership(key)
	if err != nil {
		return "", err
	}
	if present {
		return RuntimePresent, nil
	}
	return RuntimeAbsent, nil
}
