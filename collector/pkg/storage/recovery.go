package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

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

	// 3. 逐一应用投影
	for _, rawJSON := range eventJSONs {
		var ev types.CollectorEvent
		if err := json.Unmarshal([]byte(rawJSON), &ev); err != nil {
			return fmt.Errorf("failed to unmarshal journal event during rebuild: %w", err)
		}

		if err := ApplyEventProjection(ctx, tx, &ev); err != nil {
			return fmt.Errorf("failed to apply projection for event %s during rebuild: %w", ev.EventID, err)
		}
	}

	return tx.Commit()
}
