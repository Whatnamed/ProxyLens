-- Migration 007: Collector Heartbeat & Runtime Liveness

ALTER TABLE collector_sessions ADD COLUMN last_heartbeat_at TEXT NULL;
ALTER TABLE collector_sessions ADD COLUMN heartbeat_interval_ms INTEGER NULL;

-- 确定性初始化已有会话的心跳
UPDATE collector_sessions
SET last_heartbeat_at = COALESCE(ended_at, last_event_at, started_at),
    heartbeat_interval_ms = 5000
WHERE last_heartbeat_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_collector_sessions_liveness
ON collector_sessions(status, last_heartbeat_at);
