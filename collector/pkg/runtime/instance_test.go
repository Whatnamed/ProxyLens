package runtime

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeInstanceKeyIsStableAndDoesNotContainRawPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authority", DatabaseFileName)
	variant := strings.ToUpper(filepath.Join(dir, "authority", DatabaseFileName))

	key, err := RuntimeInstanceKey(path)
	if err != nil {
		t.Fatalf("RuntimeInstanceKey failed: %v", err)
	}
	variantKey, err := RuntimeInstanceKey(variant)
	if err != nil {
		t.Fatalf("RuntimeInstanceKey variant failed: %v", err)
	}
	if key != variantKey {
		t.Fatalf("case-normalized paths produced different keys: %q vs %q", key, variantKey)
	}
	if !strings.HasPrefix(key, `Local\ProxyLens.Runtime.v1.`) {
		t.Fatalf("unexpected instance key prefix: %q", key)
	}
	if strings.Contains(strings.ToLower(key), strings.ToLower(DatabaseFileName)) || strings.Contains(key, dir) {
		t.Fatalf("instance key leaked raw database path: %q", key)
	}

	otherKey, err := RuntimeInstanceKey(filepath.Join(dir, "other", DatabaseFileName))
	if err != nil {
		t.Fatalf("RuntimeInstanceKey other path failed: %v", err)
	}
	if key == otherKey {
		t.Fatal("different database paths must have different instance keys")
	}
}

func TestRuntimeInstanceKeyRejectsBlankPath(t *testing.T) {
	if _, err := RuntimeInstanceKey(" \t"); err == nil {
		t.Fatal("expected blank database path to fail")
	}
}

func TestSupervisorInstanceKeyIsStableAndSeparateFromRuntimeKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "authority.db")
	runtimeKey, err := RuntimeInstanceKey(dbPath)
	if err != nil {
		t.Fatalf("RuntimeInstanceKey failed: %v", err)
	}
	supervisorKey, err := SupervisorInstanceKey(dbPath)
	if err != nil {
		t.Fatalf("SupervisorInstanceKey failed: %v", err)
	}
	if runtimeKey == supervisorKey {
		t.Fatal("Runtime and Supervisor must use independent mutex names")
	}
	if !strings.HasPrefix(supervisorKey, `Local\ProxyLens.Supervisor.v1.`) {
		t.Fatalf("unexpected Supervisor key prefix: %q", supervisorKey)
	}
	if strings.Contains(supervisorKey, dbPath) {
		t.Fatalf("Supervisor key leaked raw database path: %q", supervisorKey)
	}
}
