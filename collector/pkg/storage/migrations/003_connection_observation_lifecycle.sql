-- 003_connection_observation_lifecycle.sql: Add observation lifecycle fields to connections projection

ALTER TABLE connections ADD COLUMN observation_ended_at TEXT;
ALTER TABLE connections ADD COLUMN observation_end_reason TEXT;
ALTER TABLE connections ADD COLUMN observation_end_event_id TEXT;

CREATE INDEX IF NOT EXISTS idx_conn_obs_ended ON connections(observation_ended_at);
