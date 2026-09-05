package storage

import (
	"context"
	"strings"
	"testing"
)

// TestSingleLiveGenerationInvariant verifies the migration-009 database
// invariant: at most one generation may exist in a live status (seeding,
// materializing, active); failed and superseded generations are unlimited and
// a superseded generation can be replaced by a new live one.
func TestSingleLiveGenerationInvariant(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := createAccountingTestDB(t)
	defer cleanup()

	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	insert := func(id, status string) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO accounting_generations (
				generation_id, algorithm_version, derivation_version, status,
				seed_last_sequence, seed_boundary_sequence, materialize_last_sequence,
				published_journal_sequence, notes, created_at
			) VALUES (?, ?, ?, ?, 0, 0, 0, 0, 'invariant-test', ?);
		`, id, AccountingAlgorithmVersionV2, DimensionDerivationVersion, status,
			"2026-09-05T00:00:00.000000000Z")
		return err
	}

	if err := insert("gen-active-1", "active"); err != nil {
		t.Fatalf("insert active: %v", err)
	}
	for _, live := range []string{"active", "seeding", "materializing"} {
		err := insert("gen-conflict-"+live, live)
		if err == nil {
			t.Fatalf("second live generation %q must be rejected", live)
		}
		if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "unique") {
			t.Fatalf("expected unique-constraint failure for %q, got: %v", live, err)
		}
	}
	// Non-live statuses are unlimited.
	if err := insert("gen-failed-1", "failed"); err != nil {
		t.Fatalf("failed generation must be allowed: %v", err)
	}
	if err := insert("gen-superseded-1", "superseded"); err != nil {
		t.Fatalf("superseded generation must be allowed: %v", err)
	}

	// Supersede then seed again is the controlled algorithm-repair path.
	if err := SupersedeActiveGeneration(ctx, db); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if err := insert("gen-active-2", "active"); err != nil {
		t.Fatalf("new live generation after supersede must be allowed: %v", err)
	}
}
