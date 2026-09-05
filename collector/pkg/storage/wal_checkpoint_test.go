package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestWALCheckpointTruncateBoundsWALFile verifies that a TRUNCATE checkpoint at
// a maintenance boundary keeps the write-ahead log from growing without bound
// on a writer-only connection: after many committed writes the WAL file must be
// truncated back to zero, with all committed rows preserved and the structured
// result reported as complete.
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
		res, err := CheckpointWAL(ctx, db, WALCheckpointTruncateMode)
		if err != nil {
			t.Fatalf("CheckpointWAL(TRUNCATE) failed: %v", err)
		}
		if !res.Complete() {
			t.Fatalf("TRUNCATE checkpoint on writer-only connection must complete: %+v", res)
		}
		if info, statErr := os.Stat(walPath); statErr == nil && info.Size() > 0 {
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
}

// TestWALCheckpointPassiveReportsStructuredResult verifies the steady-state
// PASSIVE mode returns structured telemetry and never treats reader contention
// (busy) as an error.
func TestWALCheckpointPassiveReportsStructuredResult(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wal-passive.db")

	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, "CREATE TABLE pl_wal_passive (id INTEGER PRIMARY KEY);"); err != nil {
		t.Fatalf("create probe table failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO pl_wal_passive VALUES (1);"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	res, err := CheckpointWAL(ctx, db, WALCheckpointPassive)
	if err != nil {
		t.Fatalf("CheckpointWAL(PASSIVE) failed: %v", err)
	}
	if res.LogFrames < 0 || res.CheckpointedFrames < 0 || res.CheckpointedFrames > res.LogFrames {
		t.Errorf("structured result fields out of range: %+v", res)
	}

	// Hold an open read transaction to force reader contention, then verify a
	// TRUNCATE attempt reports incomplete through the result, not an error.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin read tx failed: %v", err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pl_wal_passive;").Scan(&n); err != nil {
		t.Fatalf("read failed: %v", err)
	}

	res, err = CheckpointWAL(ctx, db, WALCheckpointTruncateMode)
	if err != nil {
		t.Fatalf("TRUNCATE under reader contention must not error: %v", err)
	}
	if res.Complete() && res.LogFrames > 0 {
		t.Errorf("TRUNCATE checkpoint with an active reader unexpectedly completed: %+v", res)
	}
}
