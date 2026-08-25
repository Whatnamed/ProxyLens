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

func deleteDerivedRowsBatched(ctx context.Context, db *sql.DB, tableName, runID string, batchSize int) (int64, error) {
	var totalDeleted int64
	for {
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		var deletedThisBatch int64
		err := execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx, fmt.Sprintf(`
				DELETE FROM %s WHERE rowid IN (
					SELECT rowid FROM %s WHERE run_id = ? LIMIT %d
				);
			`, tableName, tableName, batchSize), runID)
			if err != nil {
				return err
			}
			deletedThisBatch, _ = res.RowsAffected()
			return nil
		})
		if err != nil {
			return totalDeleted, err
		}

		totalDeleted += deletedThisBatch
		if deletedThisBatch == 0 {
			break
		}

		// 短暂让出锁给并发 Collector
		time.Sleep(2 * time.Millisecond)
	}
	return totalDeleted, nil
}

// ApplyDerivedRetention 执行派生层清理操作 (真正 row-batched 短事务删除)
func ApplyDerivedRetention(ctx context.Context, db *sql.DB, plan *RetentionPlan) (*RetentionResult, error) {
	res := RetentionResult{
		Plan:      *plan,
		AppliedAt: time.Now().UTC(),
	}

	if len(plan.RunsToDelete) == 0 {
		return &res, nil
	}

	const rowBatchSize = 1000

	for _, rid := range plan.RunsToDelete {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// 1. 分批删除 usage_hourly_dimensions
		c1, err := deleteDerivedRowsBatched(ctx, db, "usage_hourly_dimensions", rid, rowBatchSize)
		if err != nil {
			return nil, fmt.Errorf("failed to batch delete usage_hourly_dimensions for run %s: %w", rid, err)
		}
		res.DeletedAggregateRows += c1

		// 2. 分批删除 accounted_traffic
		c2, err := deleteDerivedRowsBatched(ctx, db, "accounted_traffic", rid, rowBatchSize)
		if err != nil {
			return nil, fmt.Errorf("failed to batch delete accounted_traffic for run %s: %w", rid, err)
		}
		res.DeletedTrafficRows += c2

		// 3. 分批删除 relay_relations
		c3, err := deleteDerivedRowsBatched(ctx, db, "relay_relations", rid, rowBatchSize)
		if err != nil {
			return nil, fmt.Errorf("failed to batch delete relay_relations for run %s: %w", rid, err)
		}
		res.DeletedRelayRows += c3

		// 4. 删除 accounting_runs 元数据记录 (短事务)
		err = execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DELETE FROM accounting_runs WHERE run_id = ?;", rid)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("failed to delete accounting_runs record for run %s: %w", rid, err)
		}
		res.DeletedRuns++
	}

	return &res, nil
}
