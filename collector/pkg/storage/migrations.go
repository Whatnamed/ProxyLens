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

// RunMigrations 自动应用所有未执行的 SQL 迁移
func RunMigrations(ctx context.Context, db *sql.DB) error {
	// 确保 schema_migrations 表存在
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

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations dir: %w", err)
	}

	var files []migrationFile
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
			name:    entry.Name(),
			sql:     string(content),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})

	var maxAppliedVersion int
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version ASC")
	if err != nil {
		return fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return err
		}
		applied[v] = true
		if v > maxAppliedVersion {
			maxAppliedVersion = v
		}
	}

	for _, m := range files {
		if applied[m.version] {
			continue
		}

		// 事务执行单个迁移
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin tx for migration %s: %w", m.name, err)
		}

		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s failed: %w", m.name, err)
		}

		nowStr := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)", m.version, m.name, nowStr); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", m.name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", m.name, err)
		}
	}

	return nil
}
