package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDBPathPrecedence(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "explicit.db")
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	dataDB := filepath.Join(dataDir, DatabaseFileName)
	if err := os.WriteFile(explicit, []byte("db"), 0o600); err != nil {
		t.Fatalf("WriteFile explicit failed: %v", err)
	}
	if err := os.WriteFile(dataDB, []byte("db"), 0o600); err != nil {
		t.Fatalf("WriteFile data failed: %v", err)
	}

	resolved, err := ResolveDBPath(DBPathOptions{
		DBPathEnv:       explicit,
		DataDirEnv:      dataDir,
		LocalAppData:    filepath.Join(dir, "local"),
		RequireExisting: true,
	})
	if err != nil {
		t.Fatalf("ResolveDBPath explicit failed: %v", err)
	}
	if resolved.Path != explicit || resolved.Source != DBPathSourceExplicit {
		t.Fatalf("explicit DB path did not win: %+v", resolved)
	}

	resolved, err = ResolveDBPath(DBPathOptions{
		DataDirEnv:      dataDir,
		LocalAppData:    filepath.Join(dir, "local"),
		RequireExisting: true,
	})
	if err != nil {
		t.Fatalf("ResolveDBPath data dir failed: %v", err)
	}
	if resolved.Path != dataDB || resolved.Source != DBPathSourceDataDir {
		t.Fatalf("data dir path did not win over default: %+v", resolved)
	}
}

func TestResolveDBPathDefaultAndWriterMissingFile(t *testing.T) {
	localAppData := filepath.Join("C:\\Users", "tester", "AppData", "Local")
	expected := filepath.Join(localAppData, "ProxyLens", "data", DatabaseFileName)
	resolved, err := ResolveDBPath(DBPathOptions{LocalAppData: localAppData})
	if err != nil {
		t.Fatalf("writer default resolution failed: %v", err)
	}
	if resolved.Path != expected || resolved.Source != DBPathSourceDefault {
		t.Fatalf("unexpected default resolution: %+v, expected %s", resolved, expected)
	}

	missing, err := ResolveDBPath(DBPathOptions{
		DBPathEnv:       filepath.Join(t.TempDir(), "missing.db"),
		RequireExisting: true,
	})
	if err == nil || !errors.Is(err, ErrDBNotReady) || missing.Path != "" {
		t.Fatalf("expected explicit missing DB_NOT_READY error, path=%+v err=%v", missing, err)
	}

	missing, err = ResolveDBPath(DBPathOptions{RequireExisting: true})
	if err == nil || !errors.Is(err, ErrDBNotReady) || !strings.Contains(err.Error(), LocalAppDataEnv) {
		t.Fatalf("expected missing LOCALAPPDATA error, path=%+v err=%v", missing, err)
	}
}

func TestResolveWritableDBPathCLIOverrideDoesNotRequireExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	resolved, err := ResolveWritableDBPath(path)
	if err != nil {
		t.Fatalf("ResolveWritableDBPath failed: %v", err)
	}
	if resolved.Path != path || resolved.Source != DBPathSourceCLI {
		t.Fatalf("unexpected CLI resolution: %+v", resolved)
	}
}
