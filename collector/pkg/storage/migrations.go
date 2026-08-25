package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migrationFile struct {
	version int
	name    string
	sql     string
}

// RunMigrations 自动应用所有未执行的 SQL 迁移并对未知更高版本 fail closed
func RunMigrations(ctx context.Context, db *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations dir: %w", err)
	}

	var files []migrationFile
	maxBinaryVersion := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		content, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", entry.Name(), err)
		}
		files = append(files, migrationFile{
			version: version,
			name:    parts[1],
			sql:     string(content),
		})
		if version > maxBinaryVersion {
			maxBinaryVersion = version
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})

	// 快速只读检查：如果 schema_migrations 已存在且版本已是最新，跳过所有写事务
	var tableExists int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations';").Scan(&tableExists)
	if tableExists > 0 {
		var maxDBVersion sql.NullInt64
		_ = db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations;").Scan(&maxDBVersion)
		if maxDBVersion.Valid && maxDBVersion.Int64 > int64(maxBinaryVersion) {
			return fmt.Errorf("database schema version (%d) is newer than binary supported max version (%d) - failing closed", maxDBVersion.Int64, maxBinaryVersion)
		}
		if maxDBVersion.Valid && maxDBVersion.Int64 == int64(maxBinaryVersion) {
			// 已经是最新版本，无需写操作
			return nil
		}
	}

	// 确保 schema_migrations 表存在 (写事务)
	initSQL := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL
	);
	`
	if _, err := db.ExecContext(ctx, initSQL); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// 查询已应用的迁移
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version ASC;")
	if err != nil {
		return fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	maxDBVersion := 0
	for rows.Next() {
		var ver int
		if err := rows.Scan(&ver); err != nil {
			return fmt.Errorf("failed to scan migration row: %w", err)
		}
		applied[ver] = true
		if ver > maxDBVersion {
			maxDBVersion = ver
		}
	}

	// 如果 DB 版本高于二进制包含的最大版本，fail closed
	if maxDBVersion > maxBinaryVersion {
		return fmt.Errorf("database schema version (%d) is newer than binary supported max version (%d) - failing closed", maxDBVersion, maxBinaryVersion)
	}

	// 顺序应用未执行的迁移
	for _, mf := range files {
		if applied[mf.version] {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin migration transaction for version %d: %w", mf.version, err)
		}

		// 执行 migration SQL (支持分号多语句)
		statements := strings.Split(mf.sql, ";")
		for _, stmt := range statements {
			trimmed := strings.TrimSpace(stmt)
			if trimmed == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, trimmed); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("failed to execute migration %03d_%s: %w\nStatement: %s", mf.version, mf.name, err, trimmed)
			}
		}

		// 记录已应用版本
		recordSQL := "INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?);"
		if _, err := tx.ExecContext(ctx, recordSQL, mf.version, mf.name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to record migration %03d_%s: %w", mf.version, mf.name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %03d_%s: %w", mf.version, mf.name, err)
		}
	}

	return nil
}
