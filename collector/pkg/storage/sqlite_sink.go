package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// SQLiteEventSink 提供基于 SQLite + WAL 的可靠事件持久化 Sink
type SQLiteEventSink struct {
	mu        sync.Mutex
	db        *sql.DB
	sessionID string
	dbPath    string
}

// NewSQLiteEventSink 创建 SQLiteEventSink 实例
func NewSQLiteEventSink(db *sql.DB, sessionID string, dbPath string) *SQLiteEventSink {
	return &SQLiteEventSink{
		db:        db,
		sessionID: sessionID,
		dbPath:    dbPath,
	}
}

// OpenSQLiteSink 便捷打开数据库并创建 Sink
func OpenSQLiteSink(ctx context.Context, dbPath string, sessionID string, collectorVersion string) (*SQLiteEventSink, error) {
	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	sink := NewSQLiteEventSink(db, sessionID, dbPath)
	if err := sink.BeginSession(ctx, sessionID, collectorVersion); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to begin session: %w", err)
	}

	return sink, nil
}

// BeginSession 初始化 Collector 会话并检测前序会话异常与生成离线 Gap
func (s *SQLiteEventSink) BeginSession(ctx context.Context, sessionID string, collectorVersion string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessionID = sessionID
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. 查找最近的一个历史会话
	var prevSessionID, prevStatus string
	var prevLastEventStr, prevEndedStr sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT session_id, status, last_event_at, ended_at
		FROM collector_sessions
		WHERE session_id != ?
		ORDER BY started_at DESC LIMIT 1
	`, sessionID).Scan(&prevSessionID, &prevStatus, &prevLastEventStr, &prevEndedStr)

	if err == nil {
		// 存在前序会话
		if prevStatus == string(SessionStatusRunning) {
			// 前序会话非正常退出 (Interrupted)
			if _, err := tx.ExecContext(ctx, `
				UPDATE collector_sessions SET status = 'interrupted', ended_at = ?, updated_at = ? WHERE session_id = ?
			`, nowStr, nowStr, prevSessionID); err != nil {
				return fmt.Errorf("failed to mark previous session as interrupted: %w", err)
			}

			gapStart := prevLastEventStr.String
			if gapStart == "" {
				gapStart = nowStr
			}
			gapID := fmt.Sprintf("gap-offline-%s-%s", prevSessionID, sessionID)
			gapReason := "collector_unclean_shutdown_or_process_termination"

			insertGapSQL := `
			INSERT INTO monitoring_gaps (
				gap_id, source, session_id, started_at, ended_at, reason, precision, created_at
			) VALUES (?, 'collector_session_boundary', ?, ?, ?, ?, 'interval', ?);
			`
			if _, err := tx.ExecContext(ctx, insertGapSQL, gapID, sessionID, gapStart, nowStr, gapReason, nowStr); err != nil {
				return fmt.Errorf("failed to insert offline boundary gap: %w", err)
			}
		} else if prevStatus == string(SessionStatusClosedClean) {
			// 前序会话正常退出，推导 collector_not_running 离线缺口
			gapStart := prevEndedStr.String
			if gapStart == "" {
				gapStart = prevLastEventStr.String
			}
			if gapStart != "" {
				gapID := fmt.Sprintf("gap-offline-%s-%s", prevSessionID, sessionID)
				gapReason := "collector_not_running"

				insertGapSQL := `
				INSERT INTO monitoring_gaps (
					gap_id, source, session_id, started_at, ended_at, reason, precision, created_at
				) VALUES (?, 'collector_session_boundary', ?, ?, ?, ?, 'interval', ?);
				`
				if _, err := tx.ExecContext(ctx, insertGapSQL, gapID, sessionID, gapStart, nowStr, gapReason, nowStr); err != nil {
					return fmt.Errorf("failed to insert clean boundary gap: %w", err)
				}
			}
		}
	} else if !errorsIsNoRows(err) {
		return fmt.Errorf("failed to query previous session: %w", err)
	}

	// 2. 插入当前新会话
	insertSessionSQL := `
	INSERT INTO collector_sessions (
		session_id, started_at, status, collector_version, created_at, updated_at
	) VALUES (?, ?, 'running', ?, ?, ?)
	ON CONFLICT(session_id) DO NOTHING;
	`
	if _, err := tx.ExecContext(ctx, insertSessionSQL, sessionID, nowStr, collectorVersion, nowStr, nowStr); err != nil {
		return fmt.Errorf("failed to insert new session: %w", err)
	}

	return tx.Commit()
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}

// Emit 单事务强持久化单个 CollectorEvent
func (s *SQLiteEventSink) Emit(ev *types.CollectorEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin event transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. 写入 Journal 并检查幂等与顺序
	isDuplicate, err := IngestJournalRecord(ctx, tx, ev)
	if err != nil {
		return fmt.Errorf("journal ingestion failure: %w", err)
	}

	// 2. 若非重复事件，应用投影
	if !isDuplicate {
		if err := ApplyEventProjection(ctx, tx, ev); err != nil {
			return fmt.Errorf("projection failure: %w", err)
		}
	}

	// 3. 更新 session 的 last_event_at 与 last_frame_sequence
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	obsAtStr := ev.Timestamp.UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
		UPDATE collector_sessions SET
			last_event_at = ?,
			last_frame_sequence = MAX(last_frame_sequence, ?),
			updated_at = ?
		WHERE session_id = ?;
	`, obsAtStr, ev.FrameSequence, nowStr, ev.SessionID); err != nil {
		return fmt.Errorf("failed to update session progress: %w", err)
	}

	return tx.Commit()
}

// EndSession 优雅结束会话
func (s *SQLiteEventSink) EndSession(ctx context.Context, sessionID string, status SessionStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		UPDATE collector_sessions SET
			status = ?,
			ended_at = ?,
			updated_at = ?
		WHERE session_id = ?;
	`, string(status), nowStr, nowStr, sessionID)
	return err
}

// Close 关闭底层数据库连接
func (s *SQLiteEventSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
