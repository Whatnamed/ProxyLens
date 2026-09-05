package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// OpenDB 创建/打开指定路径的 SQLite 数据库，配置 WAL 与 PRAGMA，并自动应用迁移
func OpenDB(ctx context.Context, dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("dbPath cannot be empty")
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory %s: %w", dir, err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout=10000&_pragma=foreign_keys=ON&_pragma=synchronous=NORMAL&_txlock=immediate", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database at %s: %w", dbPath, err)
	}

	// 检查 journal_mode，仅在非 WAL 模式时尝试升级为 WAL (避免多进程重复升级导致排他锁竞争)
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to check journal_mode: %w", err)
	}
	if journalMode != "wal" {
		if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL;").Scan(&journalMode); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
		if journalMode != "wal" {
			_ = db.Close()
			return nil, fmt.Errorf("expected journal_mode 'wal', got '%s'", journalMode)
		}
	}

	// 限制连接池参数以确保单写安全与多读顺畅
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	// 运行版本迁移
	if err := RunMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

// execWithTxRetry 在发生短时锁争用或快照过时 (database is locked / busy) 时自动进行退避重试。
// writer DSN 通过 _txlock=immediate 使所有事务以 BEGIN IMMEDIATE 开启：核算 chunk
// 这类"先读后写"的长事务若以 DEFERRED 升级写锁，在读阶段被并发 writer 提交后必然
// 得到不可重试的 SQLITE_BUSY_SNAPSHOT（真实生产库 5.2GB 上 100% 复现）。IMMEDIATE
// 让事务一开始就取得写锁，读阶段不再被作废；单 chunk 持锁 ~1s，Collector 写入
// 在 busy_timeout 内等待即可。
func execWithTxRetry(ctx context.Context, db *sql.DB, maxRetries int, fn func(tx *sql.Tx) error) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := func() error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()

			if err := fn(tx); err != nil {
				return err
			}

			return tx.Commit()
		}()

		if err == nil {
			return nil
		}

		lastErr = err
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "locked") || strings.Contains(errStr, "busy") {
			time.Sleep(time.Duration(20+15*i) * time.Millisecond)
			continue
		}

		// 非锁冲突错误直接退出
		return err
	}
	return fmt.Errorf("transaction failed after %d retries: %w", maxRetries, lastErr)
}

var (
	ErrDBUnavailable      = fmt.Errorf("database unavailable: file does not exist or cannot be accessed")
	ErrSchemaIncompatible = fmt.Errorf("database schema incompatible")
)

// WALCheckpointMode selects the SQLite wal_checkpoint variant.
type WALCheckpointMode string

const (
	// WALCheckpointPassive is the steady-state mode: it never blocks writers
	// or readers and may checkpoint only part of the log.
	WALCheckpointPassive WALCheckpointMode = "PASSIVE"
	// WALCheckpointTruncate fully checkpoints the log and truncates the WAL
	// file to zero bytes. It is only for safe maintenance/shutdown boundaries.
	WALCheckpointTruncateMode WALCheckpointMode = "TRUNCATE"
)

// WALCheckpointResult carries the structured SQLite wal_checkpoint outcome.
// A busy or incomplete checkpoint is health evidence, not an error and not a
// success: callers must inspect Busy and CheckpointedFrames.
type WALCheckpointResult struct {
	Busy               bool
	LogFrames          int64
	CheckpointedFrames int64
}

// Complete reports whether the checkpoint fully checkpointed the log without
// reader/writer contention.
func (r WALCheckpointResult) Complete() bool {
	return !r.Busy && r.LogFrames == r.CheckpointedFrames
}

// CheckpointWAL runs PRAGMA wal_checkpoint(mode) and returns the structured
// result. Committed data is never altered. A busy result (readers active) is
// reported through the returned values rather than treated as an error.
func CheckpointWAL(ctx context.Context, db *sql.DB, mode WALCheckpointMode) (WALCheckpointResult, error) {
	if db == nil {
		return WALCheckpointResult{}, fmt.Errorf("wal checkpoint requires a database")
	}
	var res WALCheckpointResult
	var busy, logFrames, checkpointedFrames int64
	row := db.QueryRowContext(ctx, fmt.Sprintf("PRAGMA wal_checkpoint(%s);", string(mode)))
	if err := row.Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		return res, fmt.Errorf("wal checkpoint(%s) failed: %w", mode, err)
	}
	res.Busy = busy != 0
	res.LogFrames = logFrames
	res.CheckpointedFrames = checkpointedFrames
	return res, nil
}

// OpenReadOnlyDB 以严格只读模式打开指定 SQLite 数据库并验证模式兼容性，绝不执行迁移或写操作
func OpenReadOnlyDB(ctx context.Context, dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("%w: dbPath cannot be empty", ErrDBUnavailable)
	}

	// 1. 检查文件物理存在性
	if fi, err := os.Stat(dbPath); err != nil || fi.IsDir() {
		return nil, fmt.Errorf("%w: sqlite file not found at %s", ErrDBUnavailable, dbPath)
	}

	// 2. 使用标准 SQLite File URI (mode=ro + query_only=ON 双重防御) 配置 DSN
	cleanPath := filepath.ToSlash(filepath.Clean(dbPath))
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only=ON&_pragma=busy_timeout=10000&_pragma=foreign_keys=ON&_pragma=synchronous=NORMAL", cleanPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to open sqlite connection: %v", ErrDBUnavailable, err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	// 3. 严格模式兼容性检验 (只读 SELECT): 必须精确匹配当前版本
	var tableExists int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations';").Scan(&tableExists); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: failed to query sqlite_master: %v", ErrDBUnavailable, err)
	}
	if tableExists == 0 {
		_ = db.Close()
		return nil, fmt.Errorf("%w: schema_migrations table does not exist", ErrSchemaIncompatible)
	}

	maxBinary := GetMaxBinaryMigrationVersion()
	var maxDBVersion sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations;").Scan(&maxDBVersion); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: failed to query max schema version: %v", ErrSchemaIncompatible, err)
	}

	if !maxDBVersion.Valid || maxDBVersion.Int64 <= 0 {
		_ = db.Close()
		return nil, fmt.Errorf("%w: no applied migrations recorded", ErrSchemaIncompatible)
	}

	// 严格模式: 低于或高于当前版本均 fail closed
	if maxDBVersion.Int64 != int64(maxBinary) {
		_ = db.Close()
		return nil, fmt.Errorf("%w: db schema version (%d) does not match required binary schema version (%d)", ErrSchemaIncompatible, maxDBVersion.Int64, maxBinary)
	}

	return db, nil
}


