package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// RebuildProjections 清空现有派生视图并从权威 event_journal 100% 完整重建
func RebuildProjections(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin rebuild tx: %w", err)
	}
	defer tx.Rollback()

	// 1. 清空可重建表（保留 session boundary gaps）
	cleanQueries := []string{
		"DELETE FROM connections;",
		"DELETE FROM connection_traffic;",
		"DELETE FROM residual_intervals;",
		"DELETE FROM collector_health;",
		"DELETE FROM monitoring_gaps WHERE source = 'controller_stream';",
	}
	for _, q := range cleanQueries {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("failed to clean projection table (%s): %w", q, err)
		}
	}

	// 2. 从 journal 权威排序读取所有事件
	querySQL := `
	SELECT event_json FROM event_journal
	ORDER BY session_id ASC, epoch_id ASC, frame_sequence ASC, event_sequence ASC;
	`
	rows, err := tx.QueryContext(ctx, querySQL)
	if err != nil {
		return fmt.Errorf("failed to query event journal: %w", err)
	}
	defer rows.Close()

	var eventJSONs []string
	for rows.Next() {
		var ej string
		if err := rows.Scan(&ej); err != nil {
			return fmt.Errorf("failed to scan event json: %w", err)
		}
		eventJSONs = append(eventJSONs, ej)
	}

	// 3. 逐一应用投影 (使用 json.Decoder + UseNumber 保持大整数精度)
	for _, rawJSON := range eventJSONs {
		var ev types.CollectorEvent
		decoder := json.NewDecoder(strings.NewReader(rawJSON))
		decoder.UseNumber()
		if err := decoder.Decode(&ev); err != nil {
			return fmt.Errorf("failed to decode journal event during rebuild: %w", err)
		}

		if err := ApplyEventProjection(ctx, tx, &ev); err != nil {
			return fmt.Errorf("failed to apply projection for event %s during rebuild: %w", ev.EventID, err)
		}
	}

	// 4. 从 collector_sessions 重新推导并关闭已停止/中断会话的未闭合连接观察生命周期
	sessRows, err := tx.QueryContext(ctx, `SELECT session_id, status, started_at, ended_at, last_event_at FROM collector_sessions`)
	if err != nil {
		return fmt.Errorf("failed to query sessions during rebuild: %w", err)
	}
	defer sessRows.Close()

	type sessionLifecycle struct {
		id, status, startedAt string
		endedAt, lastEventAt  sql.NullString
	}
	var sessions []sessionLifecycle
	for sessRows.Next() {
		var s sessionLifecycle
		if err := sessRows.Scan(&s.id, &s.status, &s.startedAt, &s.endedAt, &s.lastEventAt); err != nil {
			return fmt.Errorf("failed to scan session lifecycle during rebuild: %w", err)
		}
		sessions = append(sessions, s)
	}

	for _, s := range sessions {
		if s.status == string(SessionStatusClosedClean) {
			endTime := s.endedAt.String
			if endTime == "" {
				endTime = s.lastEventAt.String
			}
			if endTime == "" {
				endTime = s.startedAt
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE connections SET
					observation_ended_at = ?,
					observation_end_reason = 'collector_session_closed'
				WHERE session_id = ? AND observation_ended_at IS NULL;
			`, endTime, s.id); err != nil {
				return fmt.Errorf("failed to reconcile clean session close during rebuild: %w", err)
			}
		} else if s.status == string(SessionStatusInterrupted) {
			endTime := s.endedAt.String
			if endTime == "" {
				endTime = s.lastEventAt.String
			}
			if endTime == "" {
				endTime = s.startedAt
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE connections SET
					observation_ended_at = ?,
					observation_end_reason = 'collector_session_interrupted'
				WHERE session_id = ? AND observation_ended_at IS NULL;
			`, endTime, s.id); err != nil {
				return fmt.Errorf("failed to reconcile interrupted session close during rebuild: %w", err)
			}
		}
	}

	return tx.Commit()
}
