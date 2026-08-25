-- 002_add_traffic_sequences.sql: Add frame_sequence and event_sequence columns to connection_traffic projection

ALTER TABLE connection_traffic ADD COLUMN frame_sequence INTEGER NOT NULL DEFAULT 0;
ALTER TABLE connection_traffic ADD COLUMN event_sequence INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_traffic_conn_seq ON connection_traffic(session_id, epoch_id, connection_id, frame_sequence, event_sequence);
