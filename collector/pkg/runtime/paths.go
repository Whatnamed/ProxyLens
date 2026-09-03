package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DBPathEnv        = "PROXYLENS_DB_PATH"
	DataDirEnv       = "PROXYLENS_DATA_DIR"
	LocalAppDataEnv  = "LOCALAPPDATA"
	DatabaseFileName = "proxylens.db"
)

var ErrDBNotReady = errors.New("ProxyLens database is not ready")

type DBPathSource string

const (
	DBPathSourceExplicit = "PROXYLENS_DB_PATH"
	DBPathSourceDataDir  = "PROXYLENS_DATA_DIR"
	DBPathSourceDefault  = "LOCALAPPDATA_DEFAULT"
	DBPathSourceCLI      = "CLI"
)

type DBPathResolution struct {
	Path   string
	Source DBPathSource
}

// DBPathOptions is deliberately value-based so tests can verify the path
// contract without mutating process-wide environment variables.
type DBPathOptions struct {
	DBPathEnv       string
	DataDirEnv      string
	LocalAppData    string
	RequireExisting bool
}

// ResolveDBPath applies the formal environment path contract:
// PROXYLENS_DB_PATH, then PROXYLENS_DATA_DIR\proxylens.db, then
// %LOCALAPPDATA%\ProxyLens\data\proxylens.db.
func ResolveDBPath(opts DBPathOptions) (DBPathResolution, error) {
	if value := strings.TrimSpace(opts.DBPathEnv); value != "" {
		path := filepath.Clean(opts.DBPathEnv)
		if opts.RequireExisting && !isRegularFile(path) {
			return DBPathResolution{}, fmt.Errorf("%w: %s is set to '%s' but the file does not exist", ErrDBNotReady, DBPathEnv, opts.DBPathEnv)
		}
		return DBPathResolution{Path: path, Source: DBPathSourceExplicit}, nil
	}

	if value := strings.TrimSpace(opts.DataDirEnv); value != "" {
		path := filepath.Join(opts.DataDirEnv, DatabaseFileName)
		if opts.RequireExisting && !isRegularFile(path) {
			return DBPathResolution{}, fmt.Errorf("%w: %s resolves to '%s' but the database file does not exist", ErrDBNotReady, DataDirEnv, path)
		}
		return DBPathResolution{Path: filepath.Clean(path), Source: DBPathSourceDataDir}, nil
	}

	if value := strings.TrimSpace(opts.LocalAppData); value != "" {
		path := filepath.Join(opts.LocalAppData, "ProxyLens", "data", DatabaseFileName)
		if opts.RequireExisting && !isRegularFile(path) {
			return DBPathResolution{}, fmt.Errorf("%w: canonical database is not initialized at '%s'", ErrDBNotReady, path)
		}
		return DBPathResolution{Path: filepath.Clean(path), Source: DBPathSourceDefault}, nil
	}

	return DBPathResolution{}, fmt.Errorf("%w: %s is not set, so the canonical local data path cannot be resolved", ErrDBNotReady, LocalAppDataEnv)
}

func ResolveDBPathFromEnvironment(requireExisting bool) (DBPathResolution, error) {
	return ResolveDBPath(DBPathOptions{
		DBPathEnv:       os.Getenv(DBPathEnv),
		DataDirEnv:      os.Getenv(DataDirEnv),
		LocalAppData:    os.Getenv(LocalAppDataEnv),
		RequireExisting: requireExisting,
	})
}

// ResolveWritableDBPath gives the runtime CLI's --db flag a direct explicit
// path while keeping the documented environment/default resolver for the
// no-flag case. OpenDB is responsible for creating the writer DB and parent
// directory.
func ResolveWritableDBPath(cliDBPath string) (DBPathResolution, error) {
	if strings.TrimSpace(cliDBPath) != "" {
		return DBPathResolution{Path: filepath.Clean(cliDBPath), Source: DBPathSourceCLI}, nil
	}
	return ResolveDBPathFromEnvironment(false)
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
