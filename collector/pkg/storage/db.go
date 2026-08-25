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

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout=10000&_pragma=foreign_keys=ON&_pragma=synchronous=NORMAL", dbPath)
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

// execWithTxRetry 在发生短时锁争用或快照过时 (database is locked / busy) 时自动进行退避重试
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
