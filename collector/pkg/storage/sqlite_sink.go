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
			// 前序会话非正常退出 (未显式关闭，状态仍为 running)
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

			// 同时关闭旧 session 所有 open observations (时间用 gapStart，绝不用新 session start 冒充)
			if _, err := tx.ExecContext(ctx, `
				UPDATE connections SET
					observation_ended_at = ?,
					observation_end_reason = 'collector_session_interrupted',
					updated_at = ?
				WHERE session_id = ? AND observation_ended_at IS NULL;
			`, gapStart, nowStr, prevSessionID); err != nil {
				return fmt.Errorf("failed to close open observations for interrupted session: %w", err)
			}
		} else if prevStatus == string(SessionStatusInterrupted) {
			// 前序会话已被标记为 interrupted (例如显式 EndSession(interrupted))
			gapStart := prevEndedStr.String
			if gapStart == "" {
				gapStart = prevLastEventStr.String
			}
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
				return fmt.Errorf("failed to insert interrupted boundary gap: %w", err)
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

	// 2. 若是重复事件，整个事务直接 commit 退出 (No-op，不推进 session progress 与 last_event_at)
	if isDuplicate {
		return tx.Commit()
	}

	// 3. 应用投影
	if err := ApplyEventProjection(ctx, tx, ev); err != nil {
		return fmt.Errorf("projection failure: %w", err)
	}

	// 4. 更新 session 的 last_event_at 与 last_frame_sequence
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

// EndSession 优雅结束会话并关闭所有尚未结束观察的连接
func (s *SQLiteEventSink) EndSession(ctx context.Context, sessionID string, status SessionStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE collector_sessions SET
			status = ?,
			ended_at = ?,
			updated_at = ?
		WHERE session_id = ?;
	`, string(status), nowStr, nowStr, sessionID); err != nil {
		return err
	}

	endReason := "collector_session_closed"
	if status == SessionStatusInterrupted {
		endReason = "collector_session_interrupted"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE connections SET
			observation_ended_at = ?,
			observation_end_reason = ?,
			updated_at = ?
		WHERE session_id = ? AND observation_ended_at IS NULL;
	`, nowStr, endReason, nowStr, sessionID); err != nil {
		return err
	}

	return tx.Commit()
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
