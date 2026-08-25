package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

// PlanDerivedRetention 计算安全派生层保留清理计划 (F5)
// 默认规则: 保留最新的 retainCompleted 个 completed runs, 保留不超过 failedMaxAge 的 failed runs
// 绝不删除 raw authority (event_journal, connection_traffic, collector_sessions, monitoring_gaps)
func PlanDerivedRetention(ctx context.Context, db *sql.DB, retainCompleted int, failedMaxAge time.Duration, dbPath string) (*RetentionPlan, error) {
	if retainCompleted <= 0 {
		retainCompleted = 3
	}
	if failedMaxAge <= 0 {
		failedMaxAge = 7 * 24 * time.Hour
	}

	plan := RetentionPlan{
		RetainCompletedRuns:    retainCompleted,
		RetainFailedRunsMaxAge: failedMaxAge,
	}

	// 1. 查找所有超过 retainCompleted 的旧 completed runs
	rowsComp, err := db.QueryContext(ctx, `
		SELECT run_id FROM accounting_runs
		WHERE status = 'completed'
		ORDER BY started_at DESC;
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query completed runs: %w", err)
	}
	defer rowsComp.Close()

	var completedRunIDs []string
	for rowsComp.Next() {
		var id string
		if err := rowsComp.Scan(&id); err != nil {
			return nil, err
		}
		completedRunIDs = append(completedRunIDs, id)
	}
	if err := rowsComp.Err(); err != nil {
		return nil, err
	}

	if len(completedRunIDs) > retainCompleted {
		plan.RunsToDelete = append(plan.RunsToDelete, completedRunIDs[retainCompleted:]...)
	}

	// 2. 查找过期的 failed runs
	cutoffTime := time.Now().UTC().Add(-failedMaxAge).Format(time.RFC3339Nano)
	rowsFailed, err := db.QueryContext(ctx, `
		SELECT run_id FROM accounting_runs
		WHERE status = 'failed' AND started_at < ?;
	`, cutoffTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired failed runs: %w", err)
	}
	defer rowsFailed.Close()

	for rowsFailed.Next() {
		var id string
		if err := rowsFailed.Scan(&id); err != nil {
			return nil, err
		}
		plan.RunsToDelete = append(plan.RunsToDelete, id)
	}
	if err := rowsFailed.Err(); err != nil {
		return nil, err
	}

	// 3. 统计预计删除的派生行数
	for _, rid := range plan.RunsToDelete {
		var relCount, trafCount, aggCount int64
		_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM relay_relations WHERE run_id = ?;", rid).Scan(&relCount)
		_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounted_traffic WHERE run_id = ?;", rid).Scan(&trafCount)
		_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_hourly_dimensions WHERE run_id = ?;", rid).Scan(&aggCount)
		plan.EstimatedRelayRows += relCount
		plan.EstimatedTrafficRows += trafCount
		plan.EstimatedAggregateRows += aggCount
	}

	// 4. 获取 DB 与 WAL 大小
	if dbPath != "" {
		if fi, err := os.Stat(dbPath); err == nil {
			plan.DBSizeBytes = fi.Size()
		}
		if fiWal, err := os.Stat(dbPath + "-wal"); err == nil {
			plan.WALSizeBytes = fiWal.Size()
		}
	}

	return &plan, nil
}

// ApplyDerivedRetention 执行派生层清理操作 (短事务分批删除)
func ApplyDerivedRetention(ctx context.Context, db *sql.DB, plan *RetentionPlan) (*RetentionResult, error) {
	res := RetentionResult{
		Plan:      *plan,
		AppliedAt: time.Now().UTC(),
	}

	if len(plan.RunsToDelete) == 0 {
		return &res, nil
	}

	for _, rid := range plan.RunsToDelete {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		err := func() error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()

			r1, err := tx.ExecContext(ctx, "DELETE FROM usage_hourly_dimensions WHERE run_id = ?;", rid)
			if err != nil {
				return err
			}
			c1, _ := r1.RowsAffected()
			res.DeletedAggregateRows += c1

			r2, err := tx.ExecContext(ctx, "DELETE FROM accounted_traffic WHERE run_id = ?;", rid)
			if err != nil {
				return err
			}
			c2, _ := r2.RowsAffected()
			res.DeletedTrafficRows += c2

			r3, err := tx.ExecContext(ctx, "DELETE FROM relay_relations WHERE run_id = ?;", rid)
			if err != nil {
				return err
			}
			c3, _ := r3.RowsAffected()
			res.DeletedRelayRows += c3

			_, err = tx.ExecContext(ctx, "DELETE FROM accounting_runs WHERE run_id = ?;", rid)
			if err != nil {
				return err
			}
			res.DeletedRuns++

			return tx.Commit()
		}()
		if err != nil {
			return nil, fmt.Errorf("failed to delete derived data for run %s: %w", rid, err)
		}
	}

	return &res, nil
}
