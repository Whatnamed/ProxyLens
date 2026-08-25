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
	mu            sync.Mutex
	db            *sql.DB
	sessionID     string
	dbPath        string
	heartbeatDone chan struct{}
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

// BeginSession 初始化 Collector 会话并检测前序会话异常与生成离线 Gap (启动心跳)
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
		ORDER BY started_at DESC LIMIT 1;
	`, sessionID).Scan(&prevSessionID, &prevStatus, &prevLastEventStr, &prevEndedStr)

	if err == nil {
		if prevStatus == string(SessionStatusRunning) {
			if _, err := tx.ExecContext(ctx, `
				UPDATE collector_sessions SET status = 'interrupted', ended_at = ?, updated_at = ? WHERE session_id = ?;
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

			if _, err := tx.ExecContext(ctx, `
				UPDATE connections SET
					observation_ended_at = ?,
					observation_end_reason = 'collector_session_interrupted',
					updated_at = ?
				WHERE session_id = ? AND observation_ended_at IS NULL;
			`, gapStart, nowStr, prevSessionID); err != nil {
				return fmt.Errorf("failed to close open observations of interrupted session: %w", err)
			}
		} else if prevStatus == string(SessionStatusInterrupted) {
			gapStart := prevLastEventStr.String
			if gapStart == "" {
				gapStart = prevEndedStr.String
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

	// 2. 插入当前新会话 (记录初始心跳与心跳周期 5000ms)
	insertSessionSQL := `
	INSERT INTO collector_sessions (
		session_id, started_at, status, collector_version,
		last_heartbeat_at, heartbeat_interval_ms, created_at, updated_at
	) VALUES (?, ?, 'running', ?, ?, 5000, ?, ?)
	ON CONFLICT(session_id) DO NOTHING;
	`
	if _, err := tx.ExecContext(ctx, insertSessionSQL, sessionID, nowStr, collectorVersion, nowStr, nowStr, nowStr); err != nil {
		return fmt.Errorf("failed to insert new session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// 3. 启动后台轻量心跳循环 (F4)
	if s.heartbeatDone != nil {
		close(s.heartbeatDone)
	}
	s.heartbeatDone = make(chan struct{})
	go s.heartbeatLoop(sessionID, s.heartbeatDone)

	return nil
}

func (s *SQLiteEventSink) heartbeatLoop(sessionID string, done chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			nowStr := time.Now().UTC().Format(time.RFC3339Nano)
			_, _ = s.db.ExecContext(ctx, `
				UPDATE collector_sessions SET last_heartbeat_at = ?, updated_at = ?
				WHERE session_id = ? AND status = 'running';
			`, nowStr, nowStr, sessionID)
			cancel()
		}
	}
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

	return execWithTxRetry(ctx, s.db, 5, func(tx *sql.Tx) error {
		// 1. Ingest 不可变 Journal
		isDup, err := IngestJournalRecord(ctx, tx, ev)
		if err != nil {
			return fmt.Errorf("failed to ingest journal: %w", err)
		}
		if isDup {
			return nil
		}

		// 2. 投影派生事实表
		if err := ApplyEventProjection(ctx, tx, ev); err != nil {
			return fmt.Errorf("failed to project event: %w", err)
		}

		// 3. 推进 Session 进度并顺带刷新心跳
		obsAtStr := ev.Timestamp.UTC().Format(time.RFC3339Nano)
		nowStr := time.Now().UTC().Format(time.RFC3339Nano)
		updateSessionSQL := `
		UPDATE collector_sessions SET
			last_event_at = ?,
			last_heartbeat_at = ?,
			last_frame_sequence = ?,
			updated_at = ?
		WHERE session_id = ?;
		`
		if _, err := tx.ExecContext(ctx, updateSessionSQL, obsAtStr, nowStr, ev.FrameSequence, nowStr, ev.SessionID); err != nil {
			return fmt.Errorf("failed to update session progress: %w", err)
		}

		return nil
	})
}

// EndSession 显式结束当前会话并原子关闭未闭合连接
func (s *SQLiteEventSink) EndSession(ctx context.Context, sessionID string, status SessionStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.heartbeatDone != nil {
		close(s.heartbeatDone)
		s.heartbeatDone = nil
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	updateSQL := `
	UPDATE collector_sessions SET
		status = ?,
		ended_at = ?,
		last_heartbeat_at = ?,
		updated_at = ?
	WHERE session_id = ?;
	`
	if _, err := tx.ExecContext(ctx, updateSQL, string(status), nowStr, nowStr, nowStr, sessionID); err != nil {
		return err
	}

	var lastEventStr sql.NullString
	_ = tx.QueryRowContext(ctx, "SELECT last_event_at FROM collector_sessions WHERE session_id = ?;", sessionID).Scan(&lastEventStr)

	endObsTime := nowStr
	endReason := "collector_session_closed"
	if status == SessionStatusInterrupted {
		endReason = "collector_session_interrupted"
		if lastEventStr.Valid && lastEventStr.String != "" {
			endObsTime = lastEventStr.String
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE connections SET
			observation_ended_at = ?,
			observation_end_reason = ?,
			updated_at = ?
		WHERE session_id = ? AND observation_ended_at IS NULL;
	`, endObsTime, endReason, nowStr, sessionID); err != nil {
		return err
	}

	return tx.Commit()
}

// Close 关闭 Sink 并释放资源
func (s *SQLiteEventSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.heartbeatDone != nil {
		close(s.heartbeatDone)
		s.heartbeatDone = nil
	}

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
