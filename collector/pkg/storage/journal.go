package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

var (
	ErrEventIDCollision   = errors.New("event ID collision with conflicting payload")
	ErrOrderingViolation = errors.New("event sequence ordering violation")
)

func computeEventSHA256(ev *types.CollectorEvent) (string, []byte, error) {
	bytes, err := json.Marshal(ev)
	if err != nil {
		return "", nil, err
	}
	h := sha256.Sum256(bytes)
	return hex.EncodeToString(h[:]), bytes, nil
}

// IngestJournalRecord 校验并写入 event_journal 表，返回 (isDuplicate, error)
func IngestJournalRecord(ctx context.Context, tx *sql.Tx, ev *types.CollectorEvent) (bool, error) {
	shaStr, rawJSON, err := computeEventSHA256(ev)
	if err != nil {
		return false, fmt.Errorf("failed to marshal event: %w", err)
	}

	// 1. 检查 EventID 是否已存在
	var existingSHA string
	err = tx.QueryRowContext(ctx, "SELECT event_sha256 FROM event_journal WHERE event_id = ?", ev.EventID).Scan(&existingSHA)
	if err == nil {
		// EventID 存在
		if existingSHA == shaStr {
			// 幂等重复
			return true, nil
		}
		// Hash 冲突
		return false, fmt.Errorf("%w: event_id=%s has existing_hash=%s but incoming_hash=%s", ErrEventIDCollision, ev.EventID, existingSHA, shaStr)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("failed to query existing event_id: %w", err)
	}

	// 2. 检查游标单调性
	var lastEpoch, lastFrame, lastSeq int64
	err = tx.QueryRowContext(ctx, "SELECT last_epoch_id, last_frame_sequence, last_event_sequence FROM storage_cursors WHERE session_id = ?", ev.SessionID).Scan(&lastEpoch, &lastFrame, &lastSeq)
	if err == nil {
		if ev.FrameSequence == 0 && ev.EventSequence == 0 {
			// Out-of-band session-scoped evidence (such as direct disk guard trip
			// health events emitted without an active engine): inherit the cursor
			// position and advance event_sequence monotonically instead of failing.
			ev.EpochID = int(lastEpoch)
			ev.FrameSequence = lastFrame
			ev.EventSequence = lastSeq + 1
		} else {
			// 比较权威顺序
			isForward := false
			if int64(ev.EpochID) > lastEpoch {
				isForward = true
			} else if int64(ev.EpochID) == lastEpoch {
				if ev.FrameSequence > lastFrame {
					isForward = true
				} else if ev.FrameSequence == lastFrame {
					if ev.EventSequence > lastSeq {
						isForward = true
					}
				}
			}

			if !isForward {
				return false, fmt.Errorf("%w: session %s received backwards event (epoch=%d, frame=%d, seq=%d) while cursor is at (epoch=%d, frame=%d, seq=%d)",
					ErrOrderingViolation, ev.SessionID, ev.EpochID, ev.FrameSequence, ev.EventSequence, lastEpoch, lastFrame, lastSeq)
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("failed to query cursor: %w", err)
	}

	// 3. 分配全局唯一单调递增的 journal_sequence
	var nextSeq int64
	err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(journal_sequence), 0) + 1 FROM event_journal;").Scan(&nextSeq)
	if err != nil {
		return false, fmt.Errorf("failed to allocate journal sequence: %w", err)
	}

	obsAtStr := ev.Timestamp.UTC().Format(time.RFC3339Nano)
	ingestedAtStr := time.Now().UTC().Format(time.RFC3339Nano)
	connID := sql.NullString{String: ev.ConnectionID, Valid: ev.ConnectionID != ""}

	insertSQL := `
	INSERT INTO event_journal (
		event_id, session_id, epoch_id, frame_sequence, event_sequence,
		event_type, observed_at, connection_id, event_json, event_sha256, ingested_at, journal_sequence
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	if _, err := tx.ExecContext(ctx, insertSQL,
		ev.EventID, ev.SessionID, ev.EpochID, ev.FrameSequence, ev.EventSequence,
		string(ev.Type), obsAtStr, connID, string(rawJSON), shaStr, ingestedAtStr, nextSeq,
	); err != nil {
		return false, fmt.Errorf("failed to insert journal record: %w", err)
	}

	// 4. 更新游标
	cursorSQL := `
	INSERT INTO storage_cursors (session_id, last_epoch_id, last_frame_sequence, last_event_sequence, updated_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(session_id) DO UPDATE SET
		last_epoch_id = excluded.last_epoch_id,
		last_frame_sequence = excluded.last_frame_sequence,
		last_event_sequence = excluded.last_event_sequence,
		updated_at = excluded.updated_at;
	`
	if _, err := tx.ExecContext(ctx, cursorSQL, ev.SessionID, ev.EpochID, ev.FrameSequence, ev.EventSequence, ingestedAtStr); err != nil {
		return false, fmt.Errorf("failed to update cursor: %w", err)
	}

	return false, nil
}
