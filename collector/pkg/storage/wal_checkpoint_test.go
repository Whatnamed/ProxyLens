package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestWALCheckpointTruncateBoundsWALFile verifies that WALCheckpointTruncate
// keeps the write-ahead log from growing without bound on a writer-only
// connection: after many committed writes the WAL file must be truncated back
// to zero, with all committed rows preserved.
func TestWALCheckpointTruncateBoundsWALFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wal-bounds.db")

	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, "CREATE TABLE pl_wal_probe (id INTEGER PRIMARY KEY, payload TEXT NOT NULL);"); err != nil {
		t.Fatalf("create probe table failed: %v", err)
	}

	walPath := dbPath + "-wal"
	walExisted := false
	for round := 0; round < 20; round++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin failed: %v", err)
		}
		for i := 0; i < 200; i++ {
			if _, err := tx.ExecContext(ctx, "INSERT INTO pl_wal_probe (payload) VALUES (?);", make([]byte, 2048)); err != nil {
				t.Fatalf("insert failed: %v", err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit failed: %v", err)
		}
		if err := WALCheckpointTruncate(ctx, db); err != nil {
			t.Fatalf("WALCheckpointTruncate failed: %v", err)
		}
		if info, statErr := os.Stat(walPath); statErr == nil && info.Size() > 0 {
			walExisted = true
			t.Fatalf("WAL file did not truncate after checkpoint: size=%d", info.Size())
		}
	}

	var rows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pl_wal_probe;").Scan(&rows); err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if rows != 20*200 {
		t.Fatalf("committed rows changed after checkpoints: %d", rows)
	}
	_ = walExisted
}
