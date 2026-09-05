package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// Incremental accounting v2 (Phase 3S).
//
// Normal runtime accounting never rescans full history any more. Derived state
// lives in exactly one active generation whose published_journal_sequence only
// advances inside the same transaction that makes the generation's derived
// rows query-consistent. Full-history work is limited to the explicit,
// resumable, chunked seed that activates a generation.

const (
	// AccountingAlgorithmVersionV2 is the algorithm stamp of generation-based
	// incremental accounting.
	AccountingAlgorithmVersionV2 = "incremental-accounting-v2"

	// DefaultAccountingMaxEventsPerTick bounds how many journal events one
	// scheduler tick may process. Catch-up backlogs progress in chunks and
	// yield between ticks so Collector ingestion keeps writer priority.
	DefaultAccountingMaxEventsPerTick int64 = 50000

	GenerationStatusSeeding       = "seeding"
	GenerationStatusMaterializing = "materializing"
	GenerationStatusActive        = "active"
	GenerationStatusFailed        = "failed"
	GenerationStatusSuperseded    = "superseded"
)

var (
	// ErrActiveGenerationExists is returned when a new seed is requested while
	// another generation is already active.
	ErrActiveGenerationExists = errors.New("an accounting generation is already active")
	// ErrNoActiveGeneration is returned when v2 queries need an active
	// generation but none exists (the caller falls back to legacy runs).
	ErrNoActiveGeneration = errors.New("no active accounting generation")
	// ErrSeedDiskSpaceInsufficient is returned by the automatic-seed preflight
	// when the database volume cannot safely absorb the derived-state growth
	// of a full seed. Raw collection is never blocked by it; accounting stays
	// on the legacy fallback and the seed retries on later ticks.
	ErrSeedDiskSpaceInsufficient = errors.New("seed disk preflight failed: insufficient free space for v2 derived state")
)

var v2RunSeqCounter int64

// AccountingGeneration is one versioned slice of derived accounting state.
type AccountingGeneration struct {
	GenerationID               string
	AlgorithmVersion           string
	DerivationVersion          string
	Status                     string
	SeedLastSequence           int64
	SeedBoundarySequence       int64
	MaterializeLastSequence    int64
	PublishedJournalSequence   int64
	PublishedFrameTime         sql.NullString
	PublishedRawUpload         int64
	PublishedRawDownload       int64
	PublishedAccountedUpload   int64
	PublishedAccountedDownload int64
	Notes                      string
	CreatedAt                  time.Time
	ActivatedAt                *time.Time
	SupersededAt               *time.Time
}

// GetActiveAccountingGeneration returns the single active v2 generation, or
// ErrNoActiveGeneration when v2 has not been seeded/activated yet.
func GetActiveAccountingGeneration(ctx context.Context, db *sql.DB) (*AccountingGeneration, error) {
	if db == nil {
		return nil, fmt.Errorf("generation query requires a database")
	}
	row := db.QueryRowContext(ctx, `
		SELECT generation_id, algorithm_version, derivation_version, status,
		       seed_last_sequence, seed_boundary_sequence, materialize_last_sequence,
		       published_journal_sequence, published_frame_time,
		       published_raw_upload, published_raw_download,
		       published_accounted_upload, published_accounted_download,
		       notes, created_at, activated_at, superseded_at
		FROM accounting_generations
		WHERE status = 'active'
		LIMIT 1;
	`)
	return scanGeneration(row)
}

// getResumableGeneration returns the most recent in-progress (seeding or
// materializing) generation so an interrupted seed resumes instead of restarting.
func getResumableGeneration(ctx context.Context, db *sql.DB) (*AccountingGeneration, error) {
	row := db.QueryRowContext(ctx, `
		SELECT generation_id, algorithm_version, derivation_version, status,
		       seed_last_sequence, seed_boundary_sequence, materialize_last_sequence,
		       published_journal_sequence, published_frame_time,
		       published_raw_upload, published_raw_download,
		       published_accounted_upload, published_accounted_download,
		       notes, created_at, activated_at, superseded_at
		FROM accounting_generations
		WHERE status IN ('seeding', 'materializing')
		ORDER BY created_at DESC
		LIMIT 1;
	`)
	gen, err := scanGeneration(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return gen, err
}

func scanGeneration(row *sql.Row) (*AccountingGeneration, error) {
	var g AccountingGeneration
	var createdStr string
	var activatedStr, supersededStr sql.NullString
	err := row.Scan(
		&g.GenerationID, &g.AlgorithmVersion, &g.DerivationVersion, &g.Status,
		&g.SeedLastSequence, &g.SeedBoundarySequence, &g.MaterializeLastSequence,
		&g.PublishedJournalSequence, &g.PublishedFrameTime,
		&g.PublishedRawUpload, &g.PublishedRawDownload,
		&g.PublishedAccountedUpload, &g.PublishedAccountedDownload,
		&g.Notes, &createdStr, &activatedStr, &supersededStr,
	)
	if err != nil {
		return nil, err
	}
	g.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	if activatedStr.Valid && activatedStr.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, activatedStr.String); err == nil {
			g.ActivatedAt = &t
		}
	}
	if supersededStr.Valid && supersededStr.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, supersededStr.String); err == nil {
			g.SupersededAt = &t
		}
	}
	return &g, nil
}

// SupersedeActiveGeneration marks the active generation superseded so a new
// explicit seed can activate. This is the algorithm-migration / explicit
// repair path; normal runtime accounting never calls it.
func SupersedeActiveGeneration(ctx context.Context, db *sql.DB) error {
	return execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE accounting_generations SET status = 'superseded', superseded_at = ?
			WHERE status = 'active';
		`, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	})
}

// CleanupFailedGenerations deletes all derived rows of failed generations.
// Cleanup is bounded to non-active generations; raw authority is never touched.
func CleanupFailedGenerations(ctx context.Context, db *sql.DB) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT generation_id FROM accounting_generations WHERE status = 'failed';`)
	if err != nil {
		return 0, fmt.Errorf("failed to list failed generations: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	cleaned := 0
	for _, id := range ids {
		if err := deleteGenerationDerivedRows(ctx, db, id); err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}

func deleteGenerationDerivedRows(ctx context.Context, db *sql.DB, generationID string) error {
	return execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
		for _, table := range []string{
			"accounted_traffic_v2",
			"relay_relations_v2",
			"usage_hourly_dimension_conns_v2",
			"usage_hourly_dimensions_v2",
			"accounting_conn_state_v2",
			"accounting_runs_v2",
		} {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE generation_id = ?;", table), generationID); err != nil {
				return fmt.Errorf("failed to clean %s for generation %s: %w", table, generationID, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM accounting_generations WHERE generation_id = ?;`, generationID); err != nil {
			return err
		}
		return nil
	})
}

// AdvanceAccountingV2 is the single scheduler entry point for normal runtime
// accounting. It activates a new generation through the resumable chunked
// seed when none is active, then processes bounded incremental chunks of
// (published, newBoundary]. Every chunk is one transaction: derived writes and
// boundary advancement commit atomically, so cancellation or failure never
// leaves partial garbage and retries are idempotent.
func AdvanceAccountingV2(ctx context.Context, db *sql.DB, notes string, maxEvents int64) (*AccountingRunRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("accounting v2 requires a database")
	}
	if maxEvents <= 0 {
		maxEvents = DefaultAccountingMaxEventsPerTick
	}
	if notes == "" {
		notes = "runtime scheduled accounting"
	}

	active, err := GetActiveAccountingGeneration(ctx, db)
	if err != nil && !errors.Is(err, ErrNoActiveGeneration) && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if active != nil {
		return advanceIncrementalChunk(ctx, db, active, notes, maxEvents)
	}
	return advanceSeed(ctx, db, notes, maxEvents)
}

// -----------------------------------------------------------------------------
// Seed: resumable chunked activation of a new generation
// -----------------------------------------------------------------------------

func advanceSeed(ctx context.Context, db *sql.DB, notes string, maxEvents int64) (*AccountingRunRecord, error) {
	// Resume an interrupted seed when possible; otherwise clean failed
	// leftovers and start a fresh generation.
	gen, err := getResumableGeneration(ctx, db)
	if err != nil {
		return nil, err
	}
	if gen == nil {
		// Automatic background seed safety boundary: before committing to a
		// full derived-state build, verify the database volume can absorb it.
		// The check fails closed only with positive evidence of insufficient
		// space; unknown platforms or unstatable databases proceed.
		if err := checkSeedDiskPreflight(ctx, db); err != nil {
			return nil, err
		}
		if _, err := CleanupFailedGenerations(ctx, db); err != nil {
			return nil, err
		}
		gen, err = createSeedGeneration(ctx, db, notes)
		if err != nil {
			if errors.Is(err, ErrActiveGenerationExists) {
				return nil, err
			}
			return nil, err
		}
	}

	// Drive the seed phases to completion within one call when the remaining
	// range fits the chunk budget: a fresh or small database becomes active in
	// a single tick, while a large backlog advances one chunk per tick and
	// yields so Collector ingestion keeps writer priority.
	var lastRec *AccountingRunRecord
	for {
		switch gen.Status {
		case GenerationStatusSeeding:
			rec, done, err := seedConnStateChunk(ctx, db, gen, notes, maxEvents)
			if err != nil {
				return nil, err
			}
			lastRec = rec
			gen, err = reloadGeneration(ctx, db, gen.GenerationID)
			if err != nil {
				return nil, err
			}
			if !done {
				return lastRec, nil
			}
			// Seeding range exhausted: run the shared classification contract
			// over the frozen connection state and switch to materialization.
			rec, err = classifySeedGeneration(ctx, db, gen, notes)
			if err != nil {
				return nil, err
			}
			lastRec = rec
			gen, err = reloadGeneration(ctx, db, gen.GenerationID)
			if err != nil {
				return nil, err
			}
		case GenerationStatusMaterializing:
			rec, done, err := seedMaterializeChunk(ctx, db, gen, notes, maxEvents)
			if err != nil {
				return nil, err
			}
			lastRec = rec
			gen, err = reloadGeneration(ctx, db, gen.GenerationID)
			if err != nil {
				return nil, err
			}
			if !done {
				return lastRec, nil
			}
			// Materialization complete: validate invariants and activate.
			rec, err = activateSeedGeneration(ctx, db, gen, notes)
			if err != nil {
				return nil, err
			}
			lastRec = rec
			gen, err = reloadGeneration(ctx, db, gen.GenerationID)
			if err != nil {
				return nil, err
			}
		case GenerationStatusActive:
			return lastRec, nil
		default:
			return nil, fmt.Errorf("generation %s in unexpected status %q", gen.GenerationID, gen.Status)
		}
	}
}

// seedPreflightFreeFloor is the absolute minimum free space required for a
// full v2 seed regardless of database size.
const seedPreflightFreeFloor = 512 << 20

// seedPreflightDBSizeRatio is the estimated derived-state growth of a full
// seed relative to the database file size. Production measurement (Phase 3S
// revalidation: +205MB derived on a 5.2GB authority DB) observed ~4%; the
// estimate keeps a safety factor of roughly 3.75x.
const seedPreflightDBSizeRatio = 15

// checkSeedDiskPreflight verifies the volume holding the database can absorb
// the derived-state growth of a full seed: free space must be at least
// max(512MB, 15% of the current database file size). Unknown free space
// (non-Windows platform, unstatable path) never blocks the seed.
func checkSeedDiskPreflight(ctx context.Context, db *sql.DB) error {
	var file sql.NullString
	if err := db.QueryRowContext(ctx, `PRAGMA database_list;`).Scan(new(any), new(any), &file); err != nil || !file.Valid || file.String == "" {
		// In-memory or unresolvable database: treat as unknown, proceed.
		return nil
	}
	fi, err := os.Stat(file.String)
	if err != nil {
		return nil
	}
	free, ok := freeDiskBytes(filepath.Dir(file.String))
	if !ok {
		return nil
	}
	need := fi.Size() * int64(seedPreflightDBSizeRatio) / 100
	if need < seedPreflightFreeFloor {
		need = seedPreflightFreeFloor
	}
	if int64(free) < need {
		return fmt.Errorf("%w: free %d bytes < required %d bytes for %s",
			ErrSeedDiskSpaceInsufficient, free, need, file.String)
	}
	return nil
}

func reloadGeneration(ctx context.Context, db *sql.DB, generationID string) (*AccountingGeneration, error) {	row := db.QueryRowContext(ctx, `
		SELECT generation_id, algorithm_version, derivation_version, status,
		       seed_last_sequence, seed_boundary_sequence, materialize_last_sequence,
		       published_journal_sequence, published_frame_time,
		       published_raw_upload, published_raw_download,
		       published_accounted_upload, published_accounted_download,
		       notes, created_at, activated_at, superseded_at
		FROM accounting_generations WHERE generation_id = ?;
	`, generationID)
	return scanGeneration(row)
}

func createSeedGeneration(ctx context.Context, db *sql.DB, notes string) (*AccountingGeneration, error) {
	now := time.Now().UTC()
	seq := atomic.AddInt64(&v2RunSeqCounter, 1)
	genID := fmt.Sprintf("gen-%d-%d", now.UnixNano(), seq)

	var boundarySeq int64
	err := execWithTxRetry(ctx, db, 15, func(tx *sql.Tx) error {
		var activeCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounting_generations WHERE status = 'active';`).Scan(&activeCount); err != nil {
			return err
		}
		if activeCount > 0 {
			return ErrActiveGenerationExists
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence), 0) FROM event_journal;`).Scan(&boundarySeq); err != nil {
			return fmt.Errorf("failed to capture seed boundary: %w", err)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO accounting_generations (
				generation_id, algorithm_version, derivation_version, status,
				seed_last_sequence, seed_boundary_sequence, materialize_last_sequence,
				published_journal_sequence, notes, created_at
			) VALUES (?, ?, ?, 'seeding', 0, ?, 0, 0, ?, ?);
		`, genID, AccountingAlgorithmVersionV2, DimensionDerivationVersion, boundarySeq, notes, now.Format(time.RFC3339Nano))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &AccountingGeneration{
		GenerationID:         genID,
		AlgorithmVersion:     AccountingAlgorithmVersionV2,
		DerivationVersion:    DimensionDerivationVersion,
		Status:               GenerationStatusSeeding,
		SeedBoundarySequence: boundarySeq,
		Notes:                notes,
		CreatedAt:            now,
	}, nil
}

// seedConnStateChunk streams one bounded, frame-aligned journal range into the
// per-connection accounting summary state. done reports whether the seeding
// range is fully consumed and classification may proceed.
func seedConnStateChunk(ctx context.Context, db *sql.DB, gen *AccountingGeneration, notes string, maxEvents int64) (*AccountingRunRecord, bool, error) {
	from := gen.SeedLastSequence
	to, err := frameAlignedCut(ctx, db, from, gen.SeedBoundarySequence, maxEvents)
	if err != nil {
		return nil, false, err
	}
	done := to >= gen.SeedBoundarySequence

	startedAt := time.Now().UTC()
	runID := newV2RunID()

	if to > from {
		err = execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
			processed, err := applyConnStateRange(ctx, tx, gen.GenerationID, from, to)
			if err != nil {
				return err
			}
			if err := updateV2RunRecord(ctx, tx, runID, gen.GenerationID, "seed", from, to, startedAt, processed, ""); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `
				UPDATE accounting_generations SET seed_last_sequence = ? WHERE generation_id = ?;
			`, to, gen.GenerationID)
			return err
		})
		if err != nil {
			return nil, false, err
		}
	}

	rec := &AccountingRunRecord{
		RunID: runID, AlgorithmVersion: AccountingAlgorithmVersionV2,
		StartedAt: startedAt, Status: AccountingRunCompleted,
		SourceJournalSequenceMax: &to, Notes: notes + " (seed conn-state chunk)",
	}
	return rec, done, nil
}

// classifySeedGeneration runs the shared relay reconciliation over the
// generation's complete connection summary state and switches the generation
// to materialization.
func classifySeedGeneration(ctx context.Context, db *sql.DB, gen *AccountingGeneration, notes string) (*AccountingRunRecord, error) {
	startedAt := time.Now().UTC()

	conns, err := loadGenerationConnInfos(ctx, db, gen.GenerationID, gen.SeedBoundarySequence)
	if err != nil {
		return nil, err
	}

	err = execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
		if _, err := persistGroupClassifications(ctx, tx, gen.GenerationID, groupConnInfos(conns), gen.SeedBoundarySequence); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE accounting_generations SET status = 'materializing', materialize_last_sequence = 0
			WHERE generation_id = ?;
		`, gen.GenerationID)
		return err
	})
	if err != nil {
		return nil, err
	}

	completed := time.Now().UTC()
	return &AccountingRunRecord{
		RunID: gen.GenerationID + "-classify", AlgorithmVersion: AccountingAlgorithmVersionV2,
		StartedAt: startedAt, CompletedAt: &completed, Status: AccountingRunCompleted,
		SourceJournalSequenceMax: &gen.SeedBoundarySequence, Notes: notes + " (seed classification)",
	}, nil
}

// seedMaterializeChunk writes bounded chunks of derived accounted rows plus
// hourly aggregates from the generation's frozen classification. done reports
// whether materialization reached the seed boundary and activation may proceed.
func seedMaterializeChunk(ctx context.Context, db *sql.DB, gen *AccountingGeneration, notes string, maxEvents int64) (*AccountingRunRecord, bool, error) {
	from := gen.MaterializeLastSequence
	to, err := frameAlignedCut(ctx, db, from, gen.SeedBoundarySequence, maxEvents)
	if err != nil {
		return nil, false, err
	}
	if to < from {
		to = from
	}

	startedAt := time.Now().UTC()
	runID := newV2RunID()

	classMap, err := loadGenerationClasses(ctx, db, gen.GenerationID)
	if err != nil {
		return nil, false, err
	}

	if to > from {
		err = execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
			inserted, err := applyAccountedRange(ctx, tx, gen.GenerationID, from, to, classMap)
			if err != nil {
				return err
			}
			if err := updateV2RunRecord(ctx, tx, runID, gen.GenerationID, "seed", from, to, startedAt, inserted.rowCount, ""); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `
				UPDATE accounting_generations SET materialize_last_sequence = ? WHERE generation_id = ?;
			`, to, gen.GenerationID)
			return err
		})
		if err != nil {
			return nil, false, err
		}
	}

	rec := &AccountingRunRecord{
		RunID: runID, AlgorithmVersion: AccountingAlgorithmVersionV2,
		StartedAt: startedAt, Status: AccountingRunCompleted,
		SourceJournalSequenceMax: &to, Notes: notes + " (seed materialize chunk)",
	}
	return rec, to >= gen.SeedBoundarySequence, nil
}

// activateSeedGeneration validates generation invariants and activates it
// atomically. A failed generation stays inactive and cleanable; raw authority
// is untouched.
func activateSeedGeneration(ctx context.Context, db *sql.DB, gen *AccountingGeneration, notes string) (*AccountingRunRecord, error) {
	startedAt := time.Now().UTC()

	var rawUp, rawDown, accUp, accDown, negCount, rowCount int64
	var frameTime sql.NullString
	err := execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `
			SELECT
				COALESCE(SUM(raw_upload), 0), COALESCE(SUM(raw_download), 0),
				COALESCE(SUM(accounted_upload), 0), COALESCE(SUM(accounted_download), 0),
				COUNT(CASE WHEN accounted_upload < 0 OR accounted_download < 0 THEN 1 END),
				COUNT(*)
			FROM accounted_traffic_v2 WHERE generation_id = ?;
		`, gen.GenerationID).Scan(&rawUp, &rawDown, &accUp, &accDown, &negCount, &rowCount); err != nil {
			return fmt.Errorf("failed to validate seed invariants: %w", err)
		}
		if negCount > 0 {
			return fmt.Errorf("%w: negative accounted bytes in seed (%d rows)", ErrAccountingInvariantBroken, negCount)
		}
		if accUp > rawUp || accDown > rawDown {
			return fmt.Errorf("%w: seed accounted totals exceed raw totals (up: %d > %d, down: %d > %d)",
				ErrAccountingInvariantBroken, accUp, rawUp, accDown, rawDown)
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT observed_at FROM event_journal WHERE journal_sequence = ?;
		`, gen.SeedBoundarySequence).Scan(&frameTime); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		var activeCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounting_generations WHERE status = 'active';`).Scan(&activeCount); err != nil {
			return err
		}
		if activeCount > 0 {
			return ErrActiveGenerationExists
		}

		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `
			UPDATE accounting_generations SET
				status = 'active',
				published_journal_sequence = ?,
				published_frame_time = ?,
				published_raw_upload = ?, published_raw_download = ?,
				published_accounted_upload = ?, published_accounted_download = ?,
				activated_at = ?
			WHERE generation_id = ?;
		`, gen.SeedBoundarySequence, nullStringOrEmpty(frameTime), rawUp, rawDown, accUp, accDown, now.Format(time.RFC3339Nano), gen.GenerationID); err != nil {
			return err
		}
		return updateV2RunRecord(ctx, tx, newV2RunID(), gen.GenerationID, "seed", 0, gen.SeedBoundarySequence, startedAt, rowCount, "")
	})
	if err != nil {
		markGenerationFailed(ctx, db, gen.GenerationID, err)
		return nil, err
	}

	completed := time.Now().UTC()
	return &AccountingRunRecord{
		RunID: gen.GenerationID + "-activate", AlgorithmVersion: AccountingAlgorithmVersionV2,
		StartedAt: startedAt, CompletedAt: &completed, Status: AccountingRunCompleted,
		SourceJournalSequenceMax: &gen.SeedBoundarySequence,
		SourceJournalEventCount:  rowCount, Notes: notes + " (seed activated)",
	}, nil
}

func markGenerationFailed(ctx context.Context, db *sql.DB, generationID string, cause error) {
	ctxFail, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = execWithTxRetry(ctxFail, db, 5, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctxFail, `
			UPDATE accounting_generations SET status = 'failed' WHERE generation_id = ? AND status != 'active';
		`, generationID)
		return err
	})
}

// -----------------------------------------------------------------------------
// Incremental: bounded (published, newBoundary] chunks
// -----------------------------------------------------------------------------
// Collector ingestion priority is a structural property of this path, not a
// timeout budget: every expensive read/compute step (streaming the immutable
// journal range, decoding events, loading generation state, running the
// bounded-scope relay reconciliation) happens in the read-only preparation
// phase OUTSIDE the writer transaction. The writer transaction only rechecks
// the published boundary and applies the precomputed bounded mutations plus
// the atomic publish, keeping writer-lock hold time proportional to the chunk
// footprint, never to session history cardinality.
//
// Reading generation derived state outside the transaction is safe because
// derived rows and the published boundary commit in the same transaction:
// published == from implies no other actor changed derived state since the
// preparation phase read it. Any concurrent advance is detected by the
// in-transaction boundary recheck and the chunk is skipped (no fake
// completion), never double-applied.

// IncrementalChunkTelemetry is the observable evidence of one incremental
// chunk: how long the read-only preparation took, how long the writer
// transaction held the write lock, and the bounded-scope sizes. The scale
// acceptance gate and health surfaces read it to prove the writer contract.
type IncrementalChunkTelemetry struct {
	PrepDuration   time.Duration `json:"prepDuration"`
	TxDuration     time.Duration `json:"txDuration"`
	Processed      int64         `json:"processed"`
	DirtyConns     int           `json:"dirtyConns"`
	ClosureConns   int           `json:"closureConns"`
	AffectedGroups int           `json:"affectedGroups"`
	ClassChanges   int           `json:"classChanges"`
	AccountedRows  int64         `json:"accountedRows"`
	SkippedRace    bool          `json:"skippedRace"`
}

var (
	lastIncrementalChunkMu   sync.Mutex
	lastIncrementalChunkTelm *IncrementalChunkTelemetry
)

// LastIncrementalChunkTelemetry returns the telemetry of the most recent
// incremental chunk, or nil before the first chunk.
func LastIncrementalChunkTelemetry() *IncrementalChunkTelemetry {
	lastIncrementalChunkMu.Lock()
	defer lastIncrementalChunkMu.Unlock()
	if lastIncrementalChunkTelm == nil {
		return nil
	}
	cp := *lastIncrementalChunkTelm
	return &cp
}

func recordIncrementalChunkTelemetry(t *IncrementalChunkTelemetry) {
	lastIncrementalChunkMu.Lock()
	lastIncrementalChunkTelm = t
	lastIncrementalChunkMu.Unlock()
}

// classChange is one connection whose accounting class changed within a chunk.
type classChange struct {
	key      connKey
	oldClass AccountingClass
	newClass AccountingClass
}

// incrementalPrep is the read-only result of one chunk's preparation phase.
type incrementalPrep struct {
	processed      int64
	updates        map[connKey]*connStateUpdate
	affectedGroups []connGroup
	classChanges   []classChange
	relations      []RelayRelationRecord
	closureConns   int
	accounted      *accountedPlan
}

func advanceIncrementalChunk(ctx context.Context, db *sql.DB, gen *AccountingGeneration, notes string, maxEvents int64) (*AccountingRunRecord, error) {
	from := gen.PublishedJournalSequence
	to, _, err := frameAlignedIncrementalCut(ctx, db, from, maxEvents)
	if err != nil {
		return nil, err
	}
	if to <= from {
		// Nothing new to process (the freshness check raced ahead of new
		// evidence): report a no-op run instead of nil so the scheduler does
		// not treat it as a failure.
		completed := time.Now().UTC()
		return &AccountingRunRecord{
			RunID: newV2RunID(), AlgorithmVersion: AccountingAlgorithmVersionV2,
			StartedAt: completed, CompletedAt: &completed, Status: AccountingRunCompleted,
			SourceJournalSequenceMax: &from, Notes: notes + " (no new evidence)",
		}, nil
	}

	prepStart := time.Now()
	prep, err := prepareIncrementalChunk(ctx, db, gen.GenerationID, from, to)
	if err != nil {
		return nil, err
	}
	prepDuration := time.Since(prepStart)

	startedAt := time.Now().UTC()
	runID := newV2RunID()

	skipped := false
	actualBoundary := int64(0)
	var inserted *accountedInsertResult
	var correction *accountingCorrection

	tel := &IncrementalChunkTelemetry{
		PrepDuration:   prepDuration,
		Processed:      prep.processed,
		DirtyConns:     len(prep.updates),
		ClosureConns:   prep.closureConns,
		AffectedGroups: len(prep.affectedGroups),
		ClassChanges:   len(prep.classChanges),
	}

	err = execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
		skipped = false
		// Re-read the published boundary inside the transaction so concurrent
		// accounting can never double-process a range, and so a concurrent
		// advance invalidates this chunk's prepared state.
		var published int64
		if err := tx.QueryRowContext(ctx, `
			SELECT published_journal_sequence FROM accounting_generations WHERE generation_id = ?;
		`, gen.GenerationID).Scan(&published); err != nil {
			return err
		}
		if published != from {
			// Another writer already advanced the boundary: report the real
			// boundary instead of a fake completion for the unprocessed range.
			skipped = true
			actualBoundary = published
			return nil
		}

		// 1. Apply the range's raw evidence to the connection summary state.
		if err := applyConnStateUpdates(ctx, tx, gen.GenerationID, prep.updates); err != nil {
			return err
		}

		// 2. Persist class changes for the bounded dirty closure.
		if err := applyClassChanges(ctx, tx, gen.GenerationID, prep.classChanges); err != nil {
			return err
		}

		// 3. Persist the closure candidates' relay relation decisions.
		if err := applyRelayRelationsV2(ctx, tx, gen.GenerationID, prep.relations); err != nil {
			return err
		}

		// 4. Bounded corrections for classification changes (existing derived
		// rows of the changed connections).
		correction, err = applyClassCorrections(ctx, tx, gen.GenerationID, prep.classChanges)
		if err != nil {
			return err
		}

		// 5. New derived rows for the range's nonzero traffic evidence.
		inserted, err = applyAccountedPlan(ctx, tx, gen.GenerationID, prep.accounted)
		if err != nil {
			return err
		}

		// 6. Atomically publish the new boundary with the run record.
		if err := publishGenerationBoundary(ctx, tx, gen.GenerationID, to, correction, inserted); err != nil {
			return err
		}
		return updateV2RunRecord(ctx, tx, runID, gen.GenerationID, "incremental", from, to, startedAt, prep.processed, "")
	})
	tel.TxDuration = time.Since(startedAt)
	tel.SkippedRace = skipped
	if inserted != nil {
		tel.AccountedRows = inserted.rowCount
	}
	recordIncrementalChunkTelemetry(tel)

	if err != nil {
		return nil, err
	}

	completed := time.Now().UTC()
	boundary := to
	if skipped {
		boundary = actualBoundary
		notes += " (skipped: published boundary advanced concurrently)"
	}
	return &AccountingRunRecord{
		RunID: runID, AlgorithmVersion: AccountingAlgorithmVersionV2,
		StartedAt: startedAt, CompletedAt: &completed, Status: AccountingRunCompleted,
		SourceJournalSequenceMax: &boundary, Notes: notes,
	}, nil
}

// prepareIncrementalChunk performs the full read-only preparation of one
// incremental chunk: it streams the immutable journal range, aggregates
// per-connection state updates, loads the bounded dirty closure of the
// affected groups, runs the shared relay reconciliation over that closure and
// builds the accounted insert plan. It runs outside any writer transaction.
func prepareIncrementalChunk(ctx context.Context, db *sql.DB, generationID string, from, to int64) (*incrementalPrep, error) {
	updates, trafficItems, processed, err := streamJournalRange(ctx, db, from, to)
	if err != nil {
		return nil, err
	}
	prep := &incrementalPrep{
		processed: processed,
		updates:   updates,
	}

	// Affected groups = groups with connection evidence in the range.
	groupKeys := make(map[string]connGroup)
	for k := range updates {
		gk := fmt.Sprintf("%s:%d", k.sessionID, k.epochID)
		g, ok := groupKeys[gk]
		if !ok {
			g = connGroup{sessionID: k.sessionID, epochID: k.epochID}
			groupKeys[gk] = g
		}
	}
	for _, g := range groupKeys {
		prep.affectedGroups = append(prep.affectedGroups, g)
	}

	if len(updates) == 0 {
		prep.accounted = &accountedPlan{}
		return prep, nil
	}

	// Dirty closure: the chunk's own connections, every live (non-terminal)
	// connection of the affected groups, and terminal connections whose
	// disappearance is recent enough to overlap a dirty connection's window.
	// A terminal connection T can only change class through a pair with some
	// dirty connection D, which requires D.firstObs <= T.disappearedAt; with
	// cutoff = min(firstObs over dirty connections) the SQL filter below is a
	// sound superset. The cutoff timestamp is rendered with fixed-width
	// fractional digits so the lexicographic comparison is order-safe
	// (over-inclusive by at most one second boundary).
	dirtyKeys := make([]connKey, 0, len(updates))
	for k := range updates {
		dirtyKeys = append(dirtyKeys, k)
	}
	dirtyRows, err := loadConnStateRowsByKeys(ctx, db, generationID, dirtyKeys)
	if err != nil {
		return nil, err
	}
	cutoffStr := dirtyClosureCutoff(updates, dirtyRows)
	closureRows, err := loadConnStateClosure(ctx, db, generationID, prep.affectedGroups, cutoffStr)
	if err != nil {
		return nil, err
	}

	// Build the classification input: prior state merged with the chunk's
	// updates for dirty connections, prior state alone for closure partners.
	closure := make(map[connKey]*connInfo, len(dirtyRows)+len(closureRows))
	priorClass := make(map[connKey]AccountingClass, len(dirtyRows)+len(closureRows))
	for k, row := range dirtyRows {
		c := connInfoFromState("", k.sessionID, k.epochID, k.connectionID,
			row.firstObservedAt, row.lastEventAt, row.disappearedAt,
			row.route, row.attr, row.process, row.host, row.destIP, row.rule, row.rulePayload, row.chainsJSON,
			row.monUp, row.monDown, time.Time{})
		priorClass[k] = AccountingClass(row.accountingClass)
		if u := updates[k]; u != nil {
			u.applyTo(c)
		}
		closure[k] = c
	}
	for k, row := range closureRows {
		if _, exists := closure[k]; exists {
			continue
		}
		c := connInfoFromState("", k.sessionID, k.epochID, k.connectionID,
			row.firstObservedAt, row.lastEventAt, row.disappearedAt,
			row.route, row.attr, row.process, row.host, row.destIP, row.rule, row.rulePayload, row.chainsJSON,
			row.monUp, row.monDown, time.Time{})
		priorClass[k] = AccountingClass(row.accountingClass)
		closure[k] = c
	}
	// First-ever dirty connections have no prior row: their classification
	// input is built purely from the chunk's updates.
	for k, u := range updates {
		if _, exists := closure[k]; exists {
			continue
		}
		c := &connInfo{
			key:              k,
			firstObs:         u.firstObs,
			lastEvent:        u.lastEvent,
			route:            u.route,
			attributionClass: u.attribution,
			process:          u.process,
			host:             u.host,
			destIP:           u.destIP,
			rule:             u.rule,
			rulePayload:      u.rulePayload,
			chains:           u.chains,
			monitoredUp:      u.monUp,
			monitoredDown:    u.monDown,
			disappearedAt:    u.disappearedAt,
		}
		if u.lifecycle == lifecycleActive {
			c.disappearedAt = nil
		}
		c.lastObs = effectiveLastObs(c, time.Time{})
		closure[k] = c
	}
	prep.closureConns = len(closure)

	// Classify each affected group over its closure with the shared contract.
	closureGroups := make(map[string][]*connInfo)
	for k, c := range closure {
		gk := fmt.Sprintf("%s:%d", k.sessionID, k.epochID)
		closureGroups[gk] = append(closureGroups[gk], c)
	}
	classMap := make(map[connKey]AccountingClass, len(closure))
	for _, g := range prep.affectedGroups {
		gk := fmt.Sprintf("%s:%d", g.sessionID, g.epochID)
		conns := closureGroups[gk]
		boundaryTime := boundaryFrameTimeForGroup(ctx, db, &g, to)
		for _, c := range conns {
			c.lastObs = effectiveLastObs(c, boundaryTime)
		}
		classes, relations := classifyConnectionGroup(conns)
		for k, class := range classes {
			classMap[k] = class
		}
		prep.relations = append(prep.relations, relations...)
	}
	for k := range closure {
		class, ok := classMap[k]
		if !ok {
			class = ClassUnique
		}
		if old, hasPrior := priorClass[k]; !hasPrior || old != class {
			prep.classChanges = append(prep.classChanges, classChange{key: k, oldClass: old, newClass: class})
		}
		classMap[k] = class
	}

	// Accounted insert plan for the range's nonzero traffic evidence.
	prep.accounted, err = buildAccountedPlan(trafficItems, classMap)
	if err != nil {
		return nil, err
	}
	return prep, nil
}

// dirtyClosureCutoff computes min(firstObs) over the chunk's dirty
// connections, using each connection's prior first observation where one
// exists and the chunk's own first event otherwise.
func dirtyClosureCutoff(updates map[connKey]*connStateUpdate, dirtyRows map[connKey]*connStateRow) string {
	cutoff := time.Time{}
	for k, u := range updates {
		t := u.firstObs
		if row := dirtyRows[k]; row != nil {
			if priorFirst, err := time.Parse(time.RFC3339Nano, row.firstObservedAt); err == nil {
				t = priorFirst
			}
		}
		if cutoff.IsZero() || t.Before(cutoff) {
			cutoff = t
		}
	}
	if cutoff.IsZero() {
		cutoff = time.Now().UTC()
	}
	return cutoff.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

// -----------------------------------------------------------------------------
// Shared v2 primitives
// -----------------------------------------------------------------------------

func newV2RunID() string {
	now := time.Now().UTC()
	seq := atomic.AddInt64(&v2RunSeqCounter, 1)
	return fmt.Sprintf("run2-%d-%d", now.UnixNano(), seq)
}

func nullStringOrEmpty(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return ""
}

func updateV2RunRecord(ctx context.Context, tx *sql.Tx, runID, generationID, mode string, from, to int64, startedAt time.Time, processed int64, failReason string) error {
	status := "completed"
	var completedAt any = time.Now().UTC().Format(time.RFC3339Nano)
	if failReason != "" {
		status = "failed"
		completedAt = nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO accounting_runs_v2 (
			run_id, generation_id, mode, from_sequence_exclusive, to_sequence_inclusive,
			started_at, completed_at, status, processed_events, failed_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id) DO UPDATE SET
			completed_at = excluded.completed_at,
			status = excluded.status,
			processed_events = excluded.processed_events,
			failed_reason = excluded.failed_reason;
	`, runID, generationID, mode, from, to, startedAt.Format(time.RFC3339Nano), completedAt, status, processed, sql.NullString{String: failReason, Valid: failReason != ""})
	return err
}

// frameAlignedCut extends a candidate chunk cut to the last journal sequence
// of the frame containing the cut so no frame is ever split across chunk
// boundaries (relay overlap windows depend on whole-frame boundaries).
func frameAlignedCut(ctx context.Context, db *sql.DB, from, boundary, maxEvents int64) (int64, error) {
	candidate := from + maxEvents
	if candidate > boundary {
		candidate = boundary
	}
	if candidate <= from {
		return from, nil
	}
	return extendCutToFrameEnd(ctx, db, candidate, boundary)
}

// frameAlignedIncrementalCut captures the current journal max and extends the
// chunk cut to a whole-frame boundary.
func frameAlignedIncrementalCut(ctx context.Context, db *sql.DB, from, maxEvents int64) (int64, int64, error) {
	var currentMax int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(journal_sequence), 0) FROM event_journal;`).Scan(&currentMax); err != nil {
		return 0, 0, fmt.Errorf("failed to capture current journal boundary: %w", err)
	}
	if currentMax <= from {
		return from, currentMax, nil
	}
	to, err := frameAlignedCut(ctx, db, from, currentMax, maxEvents)
	return to, currentMax, err
}

func extendCutToFrameEnd(ctx context.Context, db *sql.DB, candidate, boundary int64) (int64, error) {
	var sessID string
	var frameSeq int64
	err := db.QueryRowContext(ctx, `
		SELECT session_id, frame_sequence FROM event_journal WHERE journal_sequence = ?;
	`, candidate).Scan(&sessID, &frameSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return boundary, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to locate frame at cut: %w", err)
	}
	var frameEnd int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(journal_sequence), ?) FROM event_journal
		WHERE session_id = ? AND frame_sequence = ?;
	`, candidate, sessID, frameSeq).Scan(&frameEnd); err != nil {
		return 0, err
	}
	if frameEnd > boundary {
		frameEnd = boundary
	}
	return frameEnd, nil
}

// rowQuerier abstracts *sql.DB and *sql.Tx for read-only helpers so the same
// logic serves the seed path (inside a transaction) and the incremental
// preparation phase (outside any transaction).
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// connLifecycle is the final lifecycle decision a chunk carries for one
// connection. none keeps the stored marker untouched, active is the explicit
// clear emitted by a re-observation (New/Bootstrap), terminal sets the
// disappearance marker.
type connLifecycle int

const (
	lifecycleNone     connLifecycle = 0
	lifecycleActive   connLifecycle = 1
	lifecycleTerminal connLifecycle = 2
)

// connStateUpdate is the in-memory aggregation of one chunk's events for a
// single connection, mirroring the legacy connInfo construction exactly.
type connStateUpdate struct {
	firstObs       time.Time
	lastEvent      time.Time
	disappearedAt  *time.Time
	lifecycle      connLifecycle
	route          types.RouteType
	attribution    types.AttributionClass
	process        string
	host           string
	destIP         string
	rule           string
	rulePayload    string
	chains         []string
	monUp, monDown int64
	hasUpdate      bool
}

// applyTo merges the chunk's aggregated update into the connection's prior
// durable state, producing the classification input for the boundary.
func (u *connStateUpdate) applyTo(c *connInfo) {
	c.monitoredUp += u.monUp
	c.monitoredDown += u.monDown
	if u.lastEvent.After(c.lastEvent) {
		c.lastEvent = u.lastEvent
	}
	if u.route != "" {
		c.route = u.route
	}
	if u.attribution != "" {
		c.attributionClass = u.attribution
	}
	if u.process != "" {
		c.process = u.process
	}
	if u.host != "" {
		c.host = u.host
	}
	if u.destIP != "" {
		c.destIP = u.destIP
	}
	if u.rule != "" {
		c.rule = u.rule
	}
	if u.rulePayload != "" {
		c.rulePayload = u.rulePayload
	}
	if len(u.chains) > 0 {
		c.chains = u.chains
	}
	switch u.lifecycle {
	case lifecycleActive:
		c.disappearedAt = nil
	case lifecycleTerminal:
		c.disappearedAt = u.disappearedAt
	}
}

func (u *connStateUpdate) mergeEvent(obsTime time.Time, ev *types.CollectorEvent) {
	if !u.hasUpdate {
		u.firstObs = obsTime
		u.hasUpdate = true
	}
	if obsTime.After(u.lastEvent) {
		u.lastEvent = obsTime
	}
	// Events stream in journal order, so the final lifecycle state of the
	// chunk is decided by its last lifecycle event: Disappeared after New
	// ends terminal, New/Bootstrap after Disappeared re-opens and must clear
	// the terminal marker (the old COALESCE contract could not express the
	// explicit clear).
	switch ev.Type {
	case types.EventConnectionDisappeared:
		t := obsTime
		u.disappearedAt = &t
		u.lifecycle = lifecycleTerminal
	case types.EventConnectionNew, types.EventConnectionBootstrap:
		u.disappearedAt = nil
		u.lifecycle = lifecycleActive
	}
	if ev.Route != "" {
		u.route = ev.Route
	}
	if ev.AttributionClass != "" {
		u.attribution = ev.AttributionClass
	}
	if ev.Metadata.Process != "" {
		u.process = ev.Metadata.Process
	}
	if ev.Metadata.Host != "" {
		u.host = ev.Metadata.Host
	}
	if ev.Metadata.DestinationIP != "" {
		u.destIP = ev.Metadata.DestinationIP
	}
	if ev.Rule != "" {
		u.rule = ev.Rule
	}
	if ev.RulePayload != "" {
		u.rulePayload = ev.RulePayload
	}
	if len(ev.Chains) > 0 {
		u.chains = ev.Chains
	}
	if ev.DeltaUpload > 0 || ev.DeltaDownload > 0 {
		u.monUp += ev.DeltaUpload
		u.monDown += ev.DeltaDownload
	}
}

const connStateUpsertSQL = `
	INSERT INTO accounting_conn_state_v2 (
		generation_id, session_id, epoch_id, connection_id,
		first_observed_at, last_event_at, disappeared_at,
		route, attribution_class, process, host, destination_ip, rule, rule_payload, chains_json,
		monitored_upload, monitored_download, accounting_class
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'unique')
	ON CONFLICT(generation_id, session_id, epoch_id, connection_id) DO UPDATE SET
		last_event_at = excluded.last_event_at,
		disappeared_at = CASE ?
			WHEN 1 THEN NULL
			WHEN 2 THEN excluded.disappeared_at
			ELSE accounting_conn_state_v2.disappeared_at
		END,
		route = CASE WHEN excluded.route IS NOT NULL AND excluded.route != '' THEN excluded.route ELSE accounting_conn_state_v2.route END,
		attribution_class = CASE WHEN excluded.attribution_class IS NOT NULL AND excluded.attribution_class != '' THEN excluded.attribution_class ELSE accounting_conn_state_v2.attribution_class END,
		process = CASE WHEN excluded.process IS NOT NULL AND excluded.process != '' THEN excluded.process ELSE accounting_conn_state_v2.process END,
		host = CASE WHEN excluded.host IS NOT NULL AND excluded.host != '' THEN excluded.host ELSE accounting_conn_state_v2.host END,
		destination_ip = CASE WHEN excluded.destination_ip IS NOT NULL AND excluded.destination_ip != '' THEN excluded.destination_ip ELSE accounting_conn_state_v2.destination_ip END,
		rule = CASE WHEN excluded.rule IS NOT NULL AND excluded.rule != '' THEN excluded.rule ELSE accounting_conn_state_v2.rule END,
		rule_payload = CASE WHEN excluded.rule_payload IS NOT NULL AND excluded.rule_payload != '' THEN excluded.rule_payload ELSE accounting_conn_state_v2.rule_payload END,
		chains_json = CASE WHEN excluded.chains_json IS NOT NULL AND excluded.chains_json != '' THEN excluded.chains_json ELSE accounting_conn_state_v2.chains_json END,
		monitored_upload = monitored_upload + excluded.monitored_upload,
		monitored_download = monitored_download + excluded.monitored_download;
`

// applyConnStateRange streams the journal range (from, to] inside the caller's
// transaction (seed path) and upserts the generation's connection summary
// state so cursor advancement and state stay atomic.
func applyConnStateRange(ctx context.Context, tx *sql.Tx, generationID string, from, to int64) (int64, error) {
	updates, _, processed, err := streamJournalRange(ctx, tx, from, to)
	if err != nil {
		return 0, err
	}
	if err := applyConnStateUpdates(ctx, tx, generationID, updates); err != nil {
		return processed, err
	}
	return processed, nil
}

// streamJournalRange streams the immutable journal range (from, to] in
// sequence order and aggregates per-connection state updates plus the range's
// raw traffic items (ConnectionNew/ConnectionDelta). It performs no writes and
// is used by both the seed (inside a transaction) and the incremental
// preparation phase (outside any transaction).
func streamJournalRange(ctx context.Context, q rowQuerier, from, to int64) (map[connKey]*connStateUpdate, []journalTrafficItem, int64, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT session_id, epoch_id, connection_id, event_type, observed_at, event_json, journal_sequence
		FROM event_journal INDEXED BY idx_event_journal_sequence
		WHERE journal_sequence > ? AND journal_sequence <= ?
		ORDER BY journal_sequence ASC;
	`, from, to)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to stream journal range: %w", err)
	}
	defer rows.Close()

	updates := make(map[connKey]*connStateUpdate)
	var traffic []journalTrafficItem
	var processed int64
	for rows.Next() {
		var sessID, eventType, obsAtStr, ej string
		var connID sql.NullString
		var epochID int
		var seq int64
		if err := rows.Scan(&sessID, &epochID, &connID, &eventType, &obsAtStr, &ej, &seq); err != nil {
			return nil, nil, 0, err
		}
		processed++
		if connID.Valid && connID.String != "" {
			var ev types.CollectorEvent
			dec := json.NewDecoder(strings.NewReader(ej))
			dec.UseNumber()
			if err := dec.Decode(&ev); err != nil {
				return nil, nil, 0, fmt.Errorf("failed to decode journal event: %w", err)
			}
			obsTime, _ := time.Parse(time.RFC3339Nano, obsAtStr)
			obsTime = obsTime.UTC()

			k := connKey{sessionID: sessID, epochID: epochID, connectionID: connID.String}
			u := updates[k]
			if u == nil {
				u = &connStateUpdate{}
				updates[k] = u
			}
			u.mergeEvent(obsTime, &ev)

			if eventType == string(types.EventConnectionNew) || eventType == string(types.EventConnectionDelta) {
				traffic = append(traffic, journalTrafficItem{
					eventID: ev.EventID, sessID: sessID, epochID: epochID, connID: connID.String,
					obsAtStr: obsAtStr, ej: ej, seq: seq,
				})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("error streaming journal range: %w", err)
	}
	return updates, traffic, processed, rows.Close()
}

// connStateRow is one durable accounting_conn_state_v2 row as loaded for the
// classification input.
type connStateRow struct {
	firstObservedAt string
	lastEventAt     string
	disappearedAt   sql.NullString
	route           sql.NullString
	attr            sql.NullString
	process         sql.NullString
	host            sql.NullString
	destIP          sql.NullString
	rule            sql.NullString
	rulePayload     sql.NullString
	chainsJSON      sql.NullString
	monUp           int64
	monDown         int64
	accountingClass string
}

const connStateRowColumns = `
	first_observed_at, last_event_at, disappeared_at,
	route, attribution_class, process, host, destination_ip, rule, rule_payload, chains_json,
	monitored_upload, monitored_download, accounting_class`

func scanConnStateRow(row *sql.Row) (*connStateRow, error) {
	var r connStateRow
	err := row.Scan(&r.firstObservedAt, &r.lastEventAt, &r.disappearedAt,
		&r.route, &r.attr, &r.process, &r.host, &r.destIP, &r.rule, &r.rulePayload, &r.chainsJSON,
		&r.monUp, &r.monDown, &r.accountingClass)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func queryConnStateRow(ctx context.Context, q rowQuerier, generationID string, k connKey) (*connStateRow, error) {
	row := q.QueryRowContext(ctx, `
		SELECT `+connStateRowColumns+`
		FROM accounting_conn_state_v2
		WHERE generation_id = ? AND session_id = ? AND epoch_id = ? AND connection_id = ?;`,
		generationID, k.sessionID, k.epochID, k.connectionID)
	r, err := scanConnStateRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// loadConnStateRowsByKeys loads connection summary state rows for exact keys
// in bounded batches.
func loadConnStateRowsByKeys(ctx context.Context, db *sql.DB, generationID string, keys []connKey) (map[connKey]*connStateRow, error) {
	out := make(map[connKey]*connStateRow, len(keys))
	const batch = 400
	for i := 0; i < len(keys); i += batch {
		end := i + batch
		if end > len(keys) {
			end = len(keys)
		}
		batchKeys := keys[i:end]
		var sb strings.Builder
		sb.WriteString(`SELECT session_id, epoch_id, connection_id, ` + connStateRowColumns + `
			FROM accounting_conn_state_v2
			WHERE generation_id = ? AND (session_id, epoch_id, connection_id) IN (VALUES `)
		args := []any{generationID}
		for j, k := range batchKeys {
			if j > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("(?,?,?)")
			args = append(args, k.sessionID, k.epochID, k.connectionID)
		}
		sb.WriteString(");")
		rows, err := db.QueryContext(ctx, sb.String(), args...)
		if err != nil {
			return nil, err
		}
		if err := collectConnStateRows(rows, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// loadConnStateClosure loads, per affected group, every connection that is
// live (no terminal marker), whose terminal marker or last event is at least
// the cutoff, or whose terminal marker is older than its last event (an
// active connection with a stale pre-contract marker). Terminal connections
// older than the cutoff cannot pair with any dirty connection of this chunk,
// so excluding them keeps the reconciliation scope bounded without weakening
// legacy relay semantics.
func loadConnStateClosure(ctx context.Context, db *sql.DB, generationID string, groups []connGroup, cutoff string) (map[connKey]*connStateRow, error) {
	out := make(map[connKey]*connStateRow)
	for _, g := range groups {
		rows, err := db.QueryContext(ctx, `
			SELECT connection_id, `+connStateRowColumns+`
			FROM accounting_conn_state_v2
			WHERE generation_id = ? AND session_id = ? AND epoch_id = ?
			  AND (disappeared_at IS NULL OR disappeared_at >= ? OR last_event_at >= ?
			       OR last_event_at > disappeared_at);`,
			generationID, g.sessionID, g.epochID, cutoff, cutoff)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sessID, connID string
			var epochID int
			var r connStateRow
			if err := rows.Scan(&connID, &r.firstObservedAt, &r.lastEventAt, &r.disappearedAt,
				&r.route, &r.attr, &r.process, &r.host, &r.destIP, &r.rule, &r.rulePayload, &r.chainsJSON,
				&r.monUp, &r.monDown, &r.accountingClass); err != nil {
				rows.Close()
				return nil, err
			}
			out[connKey{sessionID: sessID, epochID: epochID, connectionID: connID}] = &r
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func collectConnStateRows(rows *sql.Rows, out map[connKey]*connStateRow) error {
	defer rows.Close()
	for rows.Next() {
		var sessID, connID string
		var epochID int
		var r connStateRow
		if err := rows.Scan(&sessID, &epochID, &connID, &r.firstObservedAt, &r.lastEventAt, &r.disappearedAt,
			&r.route, &r.attr, &r.process, &r.host, &r.destIP, &r.rule, &r.rulePayload, &r.chainsJSON,
			&r.monUp, &r.monDown, &r.accountingClass); err != nil {
			return err
		}
		out[connKey{sessionID: sessID, epochID: epochID, connectionID: connID}] = &r
	}
	return rows.Err()
}

// applyConnStateUpdates writes the chunk's aggregated per-connection state.
func applyConnStateUpdates(ctx context.Context, tx *sql.Tx, generationID string, updates map[connKey]*connStateUpdate) error {
	stmt, err := tx.PrepareContext(ctx, connStateUpsertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for k, u := range updates {
		var route, attr any
		if u.route != "" {
			route = string(u.route)
		}
		if u.attribution != "" {
			attr = string(u.attribution)
		}
		var chainsJSON any
		if len(u.chains) > 0 {
			b, _ := json.Marshal(u.chains)
			chainsJSON = string(b)
		}
		var disappeared any
		if u.disappearedAt != nil {
			disappeared = u.disappearedAt.Format(time.RFC3339Nano)
		}
		if _, err := stmt.ExecContext(ctx,
			generationID, k.sessionID, k.epochID, k.connectionID,
			u.firstObs.Format(time.RFC3339Nano), u.lastEvent.Format(time.RFC3339Nano), disappeared,
			route, attr, nullIfEmpty(u.process), nullIfEmpty(u.host), nullIfEmpty(u.destIP),
			nullIfEmpty(u.rule), nullIfEmpty(u.rulePayload), chainsJSON,
			u.monUp, u.monDown,
			int(u.lifecycle),
		); err != nil {
			return fmt.Errorf("failed to upsert conn state for %s: %w", k.connectionID, err)
		}
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// connInfoFromState converts a durable conn_state row into the shared
// in-memory classification input. The lastObserved contract is applied through
// effectiveLastObs; with a zero boundary the terminal marker and last event
// time are the only available facts.
func connInfoFromState(generationID string, sessID string, epochID int, connID string,
	firstObsStr, lastEventStr string, disappearedAt sql.NullString,
	route, attr, process, host, destIP, rule, rulePayload, chainsJSON sql.NullString,
	monUp, monDown int64, boundaryFrameTime time.Time) *connInfo {

	firstObs, _ := time.Parse(time.RFC3339Nano, firstObsStr)
	firstObs = firstObs.UTC()
	lastEvent, _ := time.Parse(time.RFC3339Nano, lastEventStr)
	lastEvent = lastEvent.UTC()

	c := &connInfo{
		key:              connKey{sessionID: sessID, epochID: epochID, connectionID: connID},
		firstObs:         firstObs,
		lastEvent:        lastEvent,
		route:            types.RouteType(route.String),
		attributionClass: types.AttributionClass(attr.String),
		process:          process.String,
		host:             host.String,
		destIP:           destIP.String,
		rule:             rule.String,
		rulePayload:      rulePayload.String,
		monitoredUp:      monUp,
		monitoredDown:    monDown,
	}
	if disappearedAt.Valid && disappearedAt.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, disappearedAt.String); err == nil {
			t = t.UTC()
			c.disappearedAt = &t
		}
	}
	c.lastObs = effectiveLastObs(c, boundaryFrameTime)
	if chainsJSON.Valid && chainsJSON.String != "" {
		_ = json.Unmarshal([]byte(chainsJSON.String), &c.chains)
	}
	return c
}

// loadGenerationConnInfos loads the complete connection summary state of a
// generation as classification input.
func loadGenerationConnInfos(ctx context.Context, db *sql.DB, generationID string, boundary int64) ([]*connInfo, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, epoch_id, connection_id, first_observed_at, last_event_at, disappeared_at,
		       route, attribution_class, process, host, destination_ip, rule, rule_payload, chains_json,
		       monitored_upload, monitored_download
		FROM accounting_conn_state_v2 WHERE generation_id = ?;
	`, generationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConnInfos(rows, boundary)
}

func scanConnInfos(rows *sql.Rows, boundary int64) ([]*connInfo, error) {
	var out []*connInfo
	for rows.Next() {
		var sessID, connID, firstObsStr, lastEventStr string
		var epochID int
		var disappearedAt, route, attr, process, host, destIP, rule, rulePayload, chainsJSON sql.NullString
		var monUp, monDown int64
		if err := rows.Scan(&sessID, &epochID, &connID, &firstObsStr, &lastEventStr, &disappearedAt,
			&route, &attr, &process, &host, &destIP, &rule, &rulePayload, &chainsJSON,
			&monUp, &monDown); err != nil {
			return nil, err
		}
		out = append(out, connInfoFromState("", sessID, epochID, connID,
			firstObsStr, lastEventStr, disappearedAt, route, attr, process, host, destIP, rule, rulePayload, chainsJSON,
			monUp, monDown, time.Time{}))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// boundaryFrameTime is filled per group by the caller when needed.
	_ = boundary
	return out, nil
}

type connGroup struct {
	sessionID string
	epochID   int
	conns     []*connInfo
}

func groupConnInfos(conns []*connInfo) map[string]*connGroup {
	grouped := make(map[string]*connGroup)
	for _, c := range conns {
		gk := fmt.Sprintf("%s:%d", c.key.sessionID, c.key.epochID)
		g := grouped[gk]
		if g == nil {
			g = &connGroup{sessionID: c.key.sessionID, epochID: c.key.epochID}
			grouped[gk] = g
		}
		g.conns = append(g.conns, c)
	}
	return grouped
}

// persistGroupClassifications stores the classification decisions for the
// given groups: connection classes plus per-group relay relations, replacing
// the groups' previous decisions within the generation. It returns the decided
// accounting class per connection so callers can diff corrections.
func persistGroupClassifications(ctx context.Context, tx *sql.Tx, generationID string, grouped map[string]*connGroup, boundary int64) (map[connKey]AccountingClass, error) {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	decided := make(map[connKey]AccountingClass)
	for _, g := range grouped {
		boundaryTime := boundaryFrameTimeForGroup(ctx, tx, g, boundary)
		for _, c := range g.conns {
			if !boundaryTime.IsZero() {
				c.lastObs = effectiveLastObs(c, boundaryTime)
			}
		}

		classes, relations := classifyConnectionGroup(g.conns)
		for k, class := range classes {
			decided[k] = class
		}

		for _, c := range g.conns {
			class := classes[c.key]
			if class == "" {
				class = ClassUnique
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE accounting_conn_state_v2 SET accounting_class = ?, classified_at = ?
				WHERE generation_id = ? AND session_id = ? AND epoch_id = ? AND connection_id = ?;
			`, string(class), nowStr, generationID, c.key.sessionID, c.key.epochID, c.key.connectionID); err != nil {
				return nil, fmt.Errorf("failed to persist conn class: %w", err)
			}
		}

		if _, err := tx.ExecContext(ctx, `
			DELETE FROM relay_relations_v2
			WHERE generation_id = ? AND candidate_session_id = ? AND candidate_epoch_id = ?;
		`, generationID, g.sessionID, g.epochID); err != nil {
			return nil, fmt.Errorf("failed to clear group relay relations: %w", err)
		}
		for _, rel := range relations {
			rel.RunID = generationID
			var logSess, logConn sql.NullString
			var logEpoch sql.NullInt64
			if rel.LogicalConnectionID != "" {
				logSess = sql.NullString{String: rel.LogicalSessionID, Valid: true}
				logEpoch = sql.NullInt64{Int64: int64(rel.LogicalEpochID), Valid: true}
				logConn = sql.NullString{String: rel.LogicalConnectionID, Valid: true}
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO relay_relations_v2 (
					generation_id, candidate_session_id, candidate_epoch_id, candidate_connection_id,
					logical_session_id, logical_epoch_id, logical_connection_id,
					status, evidence_json, derivation_version
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
			`, generationID, rel.CandidateSessionID, rel.CandidateEpochID, rel.CandidateConnectionID,
				logSess, logEpoch, logConn, string(rel.Status), rel.EvidenceJSON, rel.DerivationVersion); err != nil {
				return nil, fmt.Errorf("failed to persist relay relation: %w", err)
			}
		}
	}
	return decided, nil
}

// effectiveLastObs is the single shared lastObserved contract for relay
// overlap windows, applied identically by the legacy rebuild and v2:
//
//   - a terminal (disappeared, not re-opened) connection keeps its
//     disappearance/first-absent time — its observation window ended there and
//     must never extend toward the boundary;
//   - an active connection was present in the group's latest observation
//     frame, so its lastObs is the authoritative group frame time (with S1
//     sparse presence evidence the last per-connection event can be much
//     older than the frame it was actually present in);
//   - a disappearance marker older than the last durable evidence cannot be
//     terminal (the connection re-opened; also covers rows written before the
//     lifecycle-explicit contract) and is treated as active;
//   - without any frame time in range the last durable event time is the only
//     honest fallback.
func effectiveLastObs(c *connInfo, boundaryFrameTime time.Time) time.Time {
	if c.disappearedAt != nil && !c.disappearedAt.Before(c.lastEvent) {
		return *c.disappearedAt
	}
	if boundaryFrameTime.IsZero() {
		return c.lastEvent
	}
	return boundaryFrameTime
}

// boundaryFrameTimeForGroup resolves the authoritative frame timestamp of the
// group's most recent observation frame within the boundary range: the latest
// frame-derived evidence (per-connection events and per-frame sampling
// residuals). It must not key on the last connection-carrying event — with
// sparse presence evidence an idle connection can be silent for a full
// checkpoint interval while newer frames keep flowing.
func boundaryFrameTimeForGroup(ctx context.Context, q rowQuerier, g *connGroup, boundary int64) time.Time {
	var obsStr sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT observed_at FROM event_journal
		WHERE session_id = ? AND epoch_id = ? AND journal_sequence <= ?
		  AND ((connection_id IS NOT NULL AND connection_id != '') OR event_type = 'SamplingResidual')
		ORDER BY journal_sequence DESC LIMIT 1;
	`, g.sessionID, g.epochID, boundary).Scan(&obsStr)
	if err != nil || !obsStr.Valid {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, obsStr.String)
	return t.UTC()
}

// accountingCorrection carries the aggregate accounted-byte adjustment of a
// chunk's class changes so the publish step keeps the generation totals
// invariant-consistent.
type accountingCorrection struct {
	accUpDelta   int64
	accDownDelta int64
}

// applyClassChanges persists the bounded dirty closure's classification
// changes. Only connections whose class actually changed are written, so the
// writer cost is proportional to real decision churn, not group cardinality.
func applyClassChanges(ctx context.Context, tx *sql.Tx, generationID string, changes []classChange) error {
	if len(changes) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		UPDATE accounting_conn_state_v2 SET accounting_class = ?, classified_at = ?
		WHERE generation_id = ? AND session_id = ? AND epoch_id = ? AND connection_id = ?;
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	for _, ch := range changes {
		if _, err := stmt.ExecContext(ctx, string(ch.newClass), nowStr, generationID,
			ch.key.sessionID, ch.key.epochID, ch.key.connectionID); err != nil {
			return fmt.Errorf("failed to persist class change for %s: %w", ch.key.connectionID, err)
		}
	}
	return nil
}

// applyRelayRelationsV2 replaces the relay relation records of the closure
// candidates. Non-closure candidates keep their previous records, so the write
// scope stays bounded.
func applyRelayRelationsV2(ctx context.Context, tx *sql.Tx, generationID string, relations []RelayRelationRecord) error {
	if len(relations) == 0 {
		return nil
	}
	delStmt, err := tx.PrepareContext(ctx, `
		DELETE FROM relay_relations_v2
		WHERE generation_id = ? AND candidate_session_id = ? AND candidate_epoch_id = ? AND candidate_connection_id = ?;
	`)
	if err != nil {
		return err
	}
	defer delStmt.Close()
	insStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO relay_relations_v2 (
			generation_id, candidate_session_id, candidate_epoch_id, candidate_connection_id,
			logical_session_id, logical_epoch_id, logical_connection_id,
			status, evidence_json, derivation_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer insStmt.Close()

	for _, rel := range relations {
		if _, err := delStmt.ExecContext(ctx, generationID,
			rel.CandidateSessionID, rel.CandidateEpochID, rel.CandidateConnectionID); err != nil {
			return err
		}
		rel.RunID = generationID
		var logSess, logConn sql.NullString
		var logEpoch sql.NullInt64
		if rel.LogicalConnectionID != "" {
			logSess = sql.NullString{String: rel.LogicalSessionID, Valid: true}
			logEpoch = sql.NullInt64{Int64: int64(rel.LogicalEpochID), Valid: true}
			logConn = sql.NullString{String: rel.LogicalConnectionID, Valid: true}
		}
		if _, err := insStmt.ExecContext(ctx, generationID, rel.CandidateSessionID, rel.CandidateEpochID, rel.CandidateConnectionID,
			logSess, logEpoch, logConn, string(rel.Status), rel.EvidenceJSON, rel.DerivationVersion); err != nil {
			return err
		}
	}
	return nil
}

// applyClassCorrections rewrites the derived rows of connections whose class
// changed and aggregates the byte correction for the publish step. The
// correction scope is the chunk's class-change set, not the closure.
func applyClassCorrections(ctx context.Context, tx *sql.Tx, generationID string, changes []classChange) (*accountingCorrection, error) {
	correction := &accountingCorrection{}
	for _, ch := range changes {
		if err := applyClassCorrection(ctx, tx, generationID, ch.key, ch.oldClass, ch.newClass, correction); err != nil {
			return nil, err
		}
	}
	return correction, nil
}

// applyClassCorrection rewrites one connection's derived rows for a class
// change and adjusts the hourly aggregates accordingly. Byte semantics:
// confirmed_relay_duplicate accounts 0 bytes, everything else accounts raw.
func applyClassCorrection(ctx context.Context, tx *sql.Tx, generationID string, k connKey, oldClass, newClass AccountingClass, correction *accountingCorrection) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT source_event_id, observed_at, interval_start, interval_end, precision, route,
		       raw_upload, raw_download, accounted_upload, accounted_download,
		       process, host, destination_ip, network, rule, rule_payload, final_proxy, top_policy_group
		FROM accounted_traffic_v2
		WHERE generation_id = ? AND session_id = ? AND epoch_id = ? AND connection_id = ?;
	`, generationID, k.sessionID, k.epochID, k.connectionID)
	if err != nil {
		return err
	}
	type corrRow struct {
		eventID                                                                 string
		obsAtStr                                                                string
		intStart, intEnd                                                        sql.NullString
		prec, route                                                             string
		rawUp, rawDown                                                          int64
		oldAccUp, oldAccDown                                                    int64
		process, host, destIP, network, rule, rulePayload, finalProxy, topGroup sql.NullString
	}
	var rowsToFix []corrRow
	for rows.Next() {
		var r corrRow
		if err := rows.Scan(&r.eventID, &r.obsAtStr, &r.intStart, &r.intEnd, &r.prec, &r.route,
			&r.rawUp, &r.rawDown, &r.oldAccUp, &r.oldAccDown,
			&r.process, &r.host, &r.destIP, &r.network, &r.rule, &r.rulePayload, &r.finalProxy, &r.topGroup); err != nil {
			rows.Close()
			return err
		}
		rowsToFix = append(rowsToFix, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	newAcc := func(raw int64) int64 {
		if newClass == ClassConfirmedRelayDuplicate {
			return 0
		}
		return raw
	}

	for _, r := range rowsToFix {
		newUp := newAcc(r.rawUp)
		newDown := newAcc(r.rawDown)
		upDelta := newUp - r.oldAccUp
		downDelta := newDown - r.oldAccDown
		if upDelta == 0 && downDelta == 0 {
			continue
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE accounted_traffic_v2 SET accounted_upload = ?, accounted_download = ?, accounting_class = ?
			WHERE generation_id = ? AND source_event_id = ?;
		`, newUp, newDown, string(newClass), generationID, r.eventID); err != nil {
			return err
		}

		obsTime, _ := time.Parse(time.RFC3339Nano, r.obsAtStr)
		obsTime = obsTime.UTC()
		var intStart, intEnd *time.Time
		if r.intStart.Valid {
			if t, err := time.Parse(time.RFC3339Nano, r.intStart.String); err == nil {
				t = t.UTC()
				intStart = &t
			}
		}
		if r.intEnd.Valid {
			if t, err := time.Parse(time.RFC3339Nano, r.intEnd.String); err == nil {
				t = t.UTC()
				intEnd = &t
			}
		}
		allocations := allocateAccountedRowBuckets(obsTime, intStart, intEnd, r.prec, upDelta, downDelta)
		dims := accountedDimensionPairs(r.process, r.host, r.destIP, r.network, r.rule, r.rulePayload, r.finalProxy, r.topGroup)
		for _, alloc := range allocations {
			for _, d := range dims {
				exactUp, exactDown, estUp, estDown := int64(0), int64(0), int64(0), int64(0)
				if alloc.isExact {
					exactUp, exactDown = alloc.upBytes, alloc.downBytes
				} else {
					estUp, estDown = alloc.upBytes, alloc.downBytes
				}
				if err := upsertHourlyBytesV2(ctx, tx, generationID, alloc.bucketStart, d.dimType, d.dimKey, types.RouteType(r.route), exactUp, exactDown, estUp, estDown); err != nil {
					return err
				}
			}
		}
		correction.accUpDelta += upDelta
		correction.accDownDelta += downDelta
	}

	// connection_count maintenance: a confirmed connection stops counting.
	if newClass == ClassConfirmedRelayDuplicate && oldClass != ClassConfirmedRelayDuplicate {
		if err := removeConnFromHourlyCounts(ctx, tx, generationID, k); err != nil {
			return err
		}
	}
	if oldClass == ClassConfirmedRelayDuplicate && newClass != ClassConfirmedRelayDuplicate {
		if err := readdConnToHourlyCounts(ctx, tx, generationID, k); err != nil {
			return err
		}
	}
	return nil
}

func removeConnFromHourlyCounts(ctx context.Context, tx *sql.Tx, generationID string, k connKey) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT bucket_start, dimension_type, dimension_key, route FROM usage_hourly_dimension_conns_v2
		WHERE generation_id = ? AND session_id = ? AND epoch_id = ? AND connection_id = ?;
	`, generationID, k.sessionID, k.epochID, k.connectionID)
	if err != nil {
		return err
	}
	type seenKey struct {
		bucket, dimType, dimKey, route string
	}
	var keys []seenKey
	for rows.Next() {
		var s seenKey
		if err := rows.Scan(&s.bucket, &s.dimType, &s.dimKey, &s.route); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range keys {
		res, err := tx.ExecContext(ctx, `
			DELETE FROM usage_hourly_dimension_conns_v2
			WHERE generation_id = ? AND bucket_start = ? AND dimension_type = ? AND dimension_key = ? AND route = ?
			  AND session_id = ? AND epoch_id = ? AND connection_id = ?;
		`, generationID, s.bucket, s.dimType, s.dimKey, s.route, k.sessionID, k.epochID, k.connectionID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE usage_hourly_dimensions_v2 SET connection_count = connection_count - 1
				WHERE generation_id = ? AND bucket_start = ? AND dimension_type = ? AND dimension_key = ? AND route = ?;
			`, generationID, s.bucket, s.dimType, s.dimKey, s.route); err != nil {
				return err
			}
		}
	}
	return nil
}

func readdConnToHourlyCounts(ctx context.Context, tx *sql.Tx, generationID string, k connKey) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT observed_at, interval_start, interval_end, precision, route,
		       accounted_upload, accounted_download,
		       process, host, destination_ip, network, rule, rule_payload, final_proxy, top_policy_group
		FROM accounted_traffic_v2
		WHERE generation_id = ? AND session_id = ? AND epoch_id = ? AND connection_id = ?;
	`, generationID, k.sessionID, k.epochID, k.connectionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var obsAtStr string
		var intStart, intEnd sql.NullString
		var prec, route string
		var accUp, accDown int64
		var process, host, destIP, network, rule, rulePayload, finalProxy, topGroup sql.NullString
		if err := rows.Scan(&obsAtStr, &intStart, &intEnd, &prec, &route,
			&accUp, &accDown, &process, &host, &destIP, &network, &rule, &rulePayload, &finalProxy, &topGroup); err != nil {
			return err
		}
		if accUp == 0 && accDown == 0 {
			continue
		}
		obsTime, _ := time.Parse(time.RFC3339Nano, obsAtStr)
		obsTime = obsTime.UTC()
		var intStartPtr, intEndPtr *time.Time
		if intStart.Valid {
			if t, err := time.Parse(time.RFC3339Nano, intStart.String); err == nil {
				t = t.UTC()
				intStartPtr = &t
			}
		}
		if intEnd.Valid {
			if t, err := time.Parse(time.RFC3339Nano, intEnd.String); err == nil {
				t = t.UTC()
				intEndPtr = &t
			}
		}
		allocations := allocateAccountedRowBuckets(obsTime, intStartPtr, intEndPtr, prec, accUp, accDown)
		dims := accountedDimensionPairs(process, host, destIP, network, rule, rulePayload, finalProxy, topGroup)
		for _, alloc := range allocations {
			for _, d := range dims {
				inserted, err := insertHourlyConnSeenV2(ctx, tx, generationID, alloc.bucketStart, d.dimType, d.dimKey, types.RouteType(route), k)
				if err != nil {
					return err
				}
				if inserted {
					if err := bumpHourlyConnCountV2(ctx, tx, generationID, alloc.bucketStart, d.dimType, d.dimKey, types.RouteType(route), 1); err != nil {
						return err
					}
				}
			}
		}
	}
	return rows.Err()
}

func upsertHourlyBytesV2(ctx context.Context, tx *sql.Tx, generationID string, bucket time.Time, dimType, dimKey string, route types.RouteType, exactUp, exactDown, estUp, estDown int64) error {
	up := exactUp + estUp
	down := exactDown + estDown
	_, err := tx.ExecContext(ctx, `
		INSERT INTO usage_hourly_dimensions_v2 (
			generation_id, bucket_start, dimension_type, dimension_key, route,
			upload_bytes, download_bytes, connection_count,
			exact_upload_bytes, exact_download_bytes, estimated_upload_bytes, estimated_download_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)
		ON CONFLICT(generation_id, bucket_start, dimension_type, dimension_key, route) DO UPDATE SET
			upload_bytes = upload_bytes + excluded.upload_bytes,
			download_bytes = download_bytes + excluded.download_bytes,
			exact_upload_bytes = exact_upload_bytes + excluded.exact_upload_bytes,
			exact_download_bytes = exact_download_bytes + excluded.exact_download_bytes,
			estimated_upload_bytes = estimated_upload_bytes + excluded.estimated_upload_bytes,
			estimated_download_bytes = estimated_download_bytes + excluded.estimated_download_bytes;
	`, generationID, bucket.UTC().Format(time.RFC3339Nano), dimType, dimKey, string(route),
		up, down, exactUp, exactDown, estUp, estDown)
	return err
}

func insertHourlyConnSeenV2(ctx context.Context, tx *sql.Tx, generationID string, bucket time.Time, dimType, dimKey string, route types.RouteType, k connKey) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO usage_hourly_dimension_conns_v2 (
			generation_id, bucket_start, dimension_type, dimension_key, route,
			session_id, epoch_id, connection_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`, generationID, bucket.UTC().Format(time.RFC3339Nano), dimType, dimKey, string(route),
		k.sessionID, k.epochID, k.connectionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func bumpHourlyConnCountV2(ctx context.Context, tx *sql.Tx, generationID string, bucket time.Time, dimType, dimKey string, route types.RouteType, delta int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE usage_hourly_dimensions_v2 SET connection_count = connection_count + ?
		WHERE generation_id = ? AND bucket_start = ? AND dimension_type = ? AND dimension_key = ? AND route = ?;
	`, delta, generationID, bucket.UTC().Format(time.RFC3339Nano), dimType, dimKey, string(route))
	return err
}

// loadGenerationClasses loads the classification decision per connection.
func loadGenerationClasses(ctx context.Context, db *sql.DB, generationID string) (map[connKey]AccountingClass, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, epoch_id, connection_id, accounting_class FROM accounting_conn_state_v2 WHERE generation_id = ?;
	`, generationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[connKey]AccountingClass)
	for rows.Next() {
		var k connKey
		var class string
		if err := rows.Scan(&k.sessionID, &k.epochID, &k.connectionID, &class); err != nil {
			return nil, err
		}
		out[k] = AccountingClass(class)
	}
	return out, rows.Err()
}

// accountedInsertResult carries the byte deltas of newly inserted derived rows
// so the publish step keeps generation totals exact.
type accountedInsertResult struct {
	rowCount     int64
	rawUpDelta   int64
	rawDownDelta int64
	accUpDelta   int64
	accDownDelta int64
}

// journalTrafficItem is one raw traffic event (ConnectionNew/ConnectionDelta)
// of a streamed journal range, awaiting derived-row planning.
type journalTrafficItem struct {
	eventID, sessID, connID, obsAtStr, ej string
	epochID                               int
	seq                                   int64
}

// aggKeyV2 is one hourly aggregate key of the v2 dimension model.
type aggKeyV2 struct {
	bucket  time.Time
	dimType string
	dimKey  string
	route   types.RouteType
}

// accountedPlan is the fully precomputed derived-row write set of one range:
// rows to insert, hourly byte deltas and distinct-connection registrations.
// Building it is read-only computation; applying it is a bounded write.
type accountedPlan struct {
	rows       []accountedPlanRow
	byteDeltas map[aggKeyV2][4]int64
	seenPairs  map[aggKeyV2]map[connKey]bool
	result     accountedInsertResult
}

type accountedPlanRow struct {
	rec AccountedTrafficRecord
	seq int64
}

// buildAccountedPlan decodes the range's traffic items and builds the derived
// insert plan (zero-byte events are compacted away per the documented Phase 3S
// decision). No database access.
func buildAccountedPlan(items []journalTrafficItem, classMap map[connKey]AccountingClass) (*accountedPlan, error) {
	plan := &accountedPlan{
		byteDeltas: make(map[aggKeyV2][4]int64),
		seenPairs:  make(map[aggKeyV2]map[connKey]bool),
	}
	for _, it := range items {
		var ev types.CollectorEvent
		dec := json.NewDecoder(strings.NewReader(it.ej))
		dec.UseNumber()
		if err := dec.Decode(&ev); err != nil {
			return nil, fmt.Errorf("failed to decode traffic event %s: %w", it.eventID, err)
		}
		// Zero-byte derived compaction: rows that contribute no bytes are not
		// duplicated into derived storage (documented Phase 3S decision).
		if ev.DeltaUpload == 0 && ev.DeltaDownload == 0 {
			continue
		}

		k := connKey{sessionID: it.sessID, epochID: it.epochID, connectionID: ev.ConnectionID}
		rec := buildAccountedRecord(it.eventID, it.sessID, it.epochID, it.obsAtStr, &ev, classMap[k])
		plan.rows = append(plan.rows, accountedPlanRow{rec: rec, seq: it.seq})
		plan.result.rowCount++
		plan.result.rawUpDelta += rec.RawUpload
		plan.result.rawDownDelta += rec.RawDownload
		plan.result.accUpDelta += rec.AccountedUpload
		plan.result.accDownDelta += rec.AccountedDownload

		// Hourly contribution.
		dims := accountedDimensionPairs(
			sql.NullString{String: rec.Process, Valid: rec.Process != ""},
			sql.NullString{String: rec.Host, Valid: rec.Host != ""},
			sql.NullString{String: rec.DestinationIP, Valid: rec.DestinationIP != ""},
			sql.NullString{String: rec.Network, Valid: rec.Network != ""},
			sql.NullString{String: rec.Rule, Valid: rec.Rule != ""},
			sql.NullString{String: rec.RulePayload, Valid: rec.RulePayload != ""},
			sql.NullString{String: rec.FinalProxy, Valid: rec.FinalProxy != ""},
			sql.NullString{String: rec.TopPolicyGroup, Valid: rec.TopPolicyGroup != ""},
		)
		allocations := allocateAccountedRowBuckets(rec.ObservedAt, rec.IntervalStart, rec.IntervalEnd, rec.Precision, rec.AccountedUpload, rec.AccountedDownload)
		// connection_count tracks connections contributing nonzero accounted
		// bytes; zero-accounted rows (confirmed duplicates, zero-byte evidence)
		// must not register in the distinct-connection evidence.
		contributesConn := rec.AccountedUpload != 0 || rec.AccountedDownload != 0
		for _, alloc := range allocations {
			for _, d := range dims {
				key := aggKeyV2{bucket: alloc.bucketStart, dimType: d.dimType, dimKey: d.dimKey, route: rec.Route}
				prev := plan.byteDeltas[key]
				if alloc.isExact {
					plan.byteDeltas[key] = [4]int64{prev[0] + alloc.upBytes, prev[1] + alloc.downBytes, prev[2], prev[3]}
				} else {
					plan.byteDeltas[key] = [4]int64{prev[0], prev[1], prev[2] + alloc.upBytes, prev[3] + alloc.downBytes}
				}
				if contributesConn {
					if plan.seenPairs[key] == nil {
						plan.seenPairs[key] = make(map[connKey]bool)
					}
					plan.seenPairs[key][k] = true
				}
			}
		}
	}
	return plan, nil
}

// applyAccountedPlan writes a precomputed derived-row plan inside the caller's
// transaction.
func applyAccountedPlan(ctx context.Context, tx *sql.Tx, generationID string, plan *accountedPlan) (*accountedInsertResult, error) {
	result := &accountedInsertResult{}
	if plan == nil || len(plan.rows) == 0 {
		return result, nil
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO accounted_traffic_v2 (
			generation_id, source_event_id, source_journal_sequence, session_id, epoch_id, connection_id,
			observed_at, interval_start, interval_end, precision, route,
			raw_upload, raw_download, accounted_upload, accounted_download, accounting_class,
			process, process_path, host, sniff_host, destination_ip, network,
			rule, rule_payload, final_proxy, top_policy_group, dimension_derivation_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	for _, row := range plan.rows {
		rec := row.rec
		if _, err := stmt.ExecContext(ctx,
			generationID, rec.SourceEventID, row.seq, rec.SessionID, rec.EpochID, rec.ConnectionID,
			rec.ObservedAt.UTC().Format(time.RFC3339Nano),
			nullTimeStr(rec.IntervalStart), nullTimeStr(rec.IntervalEnd), rec.Precision, string(rec.Route),
			rec.RawUpload, rec.RawDownload, rec.AccountedUpload, rec.AccountedDownload, string(rec.AccountingClass),
			nullIfEmpty(rec.Process), nullIfEmpty(rec.ProcessPath), nullIfEmpty(rec.Host), nullIfEmpty(rec.SniffHost),
			nullIfEmpty(rec.DestinationIP), nullIfEmpty(rec.Network),
			nullIfEmpty(rec.Rule), nullIfEmpty(rec.RulePayload), nullIfEmpty(rec.FinalProxy), nullIfEmpty(rec.TopPolicyGroup),
			rec.DimensionDerivationVersion,
		); err != nil {
			return result, fmt.Errorf("failed to insert accounted row: %w", err)
		}
	}

	for key, bytes := range plan.byteDeltas {
		if err := upsertHourlyBytesV2(ctx, tx, generationID, key.bucket, key.dimType, key.dimKey, key.route, bytes[0], bytes[1], bytes[2], bytes[3]); err != nil {
			return result, err
		}
	}
	for key, conns := range plan.seenPairs {
		for ck := range conns {
			inserted, err := insertHourlyConnSeenV2(ctx, tx, generationID, key.bucket, key.dimType, key.dimKey, key.route, ck)
			if err != nil {
				return result, err
			}
			if inserted {
				if err := bumpHourlyConnCountV2(ctx, tx, generationID, key.bucket, key.dimType, key.dimKey, key.route, 1); err != nil {
					return result, err
				}
			}
		}
	}

	result.rowCount = plan.result.rowCount
	result.rawUpDelta = plan.result.rawUpDelta
	result.rawDownDelta = plan.result.rawDownDelta
	result.accUpDelta = plan.result.accUpDelta
	result.accDownDelta = plan.result.accDownDelta
	return result, nil
}

// applyAccountedRange derives and writes accounted rows for the range's
// nonzero traffic evidence inside the caller's transaction (seed path).
func applyAccountedRange(ctx context.Context, tx *sql.Tx, generationID string, from, to int64, classMap map[connKey]AccountingClass) (*accountedInsertResult, error) {
	// The event_type filter must never lure the planner away from the
	// sequence range index: without the hint SQLite scans the whole history
	// through idx_journal_type_obs (observed at production scale, 12s for an
	// empty range on 1.5M rows).
	rows, err := tx.QueryContext(ctx, `
		SELECT event_id, session_id, epoch_id, connection_id, observed_at, event_json, journal_sequence
		FROM event_journal INDEXED BY idx_event_journal_sequence
		WHERE journal_sequence > ? AND journal_sequence <= ?
		  AND event_type IN ('ConnectionNew', 'ConnectionDelta')
		ORDER BY journal_sequence ASC;
	`, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to stream traffic range: %w", err)
	}
	var items []journalTrafficItem
	for rows.Next() {
		var it journalTrafficItem
		if err := rows.Scan(&it.eventID, &it.sessID, &it.epochID, &it.connID, &it.obsAtStr, &it.ej, &it.seq); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	plan, err := buildAccountedPlan(items, classMap)
	if err != nil {
		return nil, err
	}
	return applyAccountedPlan(ctx, tx, generationID, plan)
}

func nullTimeStr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// publishGenerationBoundary atomically advances the published boundary together
// with the byte totals it covers, enforcing the accounting invariant.
func publishGenerationBoundary(ctx context.Context, tx *sql.Tx, generationID string, to int64, correction *accountingCorrection, inserted *accountedInsertResult) error {
	var frameTime sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT observed_at FROM event_journal WHERE journal_sequence = ?;`, to).Scan(&frameTime); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE accounting_generations SET
			published_journal_sequence = ?,
			published_frame_time = ?,
			published_raw_upload = published_raw_upload + ?,
			published_raw_download = published_raw_download + ?,
			published_accounted_upload = published_accounted_upload + ?,
			published_accounted_download = published_accounted_download + ?
		WHERE generation_id = ?;
	`, to, nullStringOrEmpty(frameTime),
		inserted.rawUpDelta, inserted.rawDownDelta,
		inserted.accUpDelta+correction.accUpDelta, inserted.accDownDelta+correction.accDownDelta,
		generationID); err != nil {
		return err
	}

	var pubRawUp, pubRawDown, pubAccUp, pubAccDown int64
	if err := tx.QueryRowContext(ctx, `
		SELECT published_raw_upload, published_raw_download, published_accounted_upload, published_accounted_download
		FROM accounting_generations WHERE generation_id = ?;
	`, generationID).Scan(&pubRawUp, &pubRawDown, &pubAccUp, &pubAccDown); err != nil {
		return err
	}
	if pubAccUp > pubRawUp || pubAccDown > pubRawDown {
		return fmt.Errorf("%w: published accounted totals exceed raw totals (up: %d > %d, down: %d > %d)",
			ErrAccountingInvariantBroken, pubAccUp, pubRawUp, pubAccDown, pubRawDown)
	}
	return nil
}
