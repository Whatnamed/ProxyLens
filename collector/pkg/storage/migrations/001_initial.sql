-- 001_initial.sql: ProxyLens Phase 2 Initial Schema

-- 1. Schema Migrations Table
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

-- 2. Collector Sessions Table
CREATE TABLE IF NOT EXISTS collector_sessions (
    session_id TEXT PRIMARY KEY,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    last_event_at TEXT,
    last_frame_sequence INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL CHECK (status IN ('running', 'closed_clean', 'interrupted')),
    collector_version TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- 3. Event Journal (Authoritative Immutable Event Log)
CREATE TABLE IF NOT EXISTS event_journal (
    event_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    epoch_id INTEGER NOT NULL,
    frame_sequence INTEGER NOT NULL,
    event_sequence INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    connection_id TEXT,
    event_json TEXT NOT NULL,
    event_sha256 TEXT NOT NULL,
    ingested_at TEXT NOT NULL,
    CONSTRAINT uq_event_sequence UNIQUE(session_id, epoch_id, frame_sequence, event_sequence)
);

CREATE INDEX IF NOT EXISTS idx_journal_seq ON event_journal(session_id, epoch_id, frame_sequence, event_sequence);
CREATE INDEX IF NOT EXISTS idx_journal_conn ON event_journal(connection_id);
CREATE INDEX IF NOT EXISTS idx_journal_obs ON event_journal(observed_at);
CREATE INDEX IF NOT EXISTS idx_journal_type_obs ON event_journal(event_type, observed_at);

-- 4. Storage Cursors Table
CREATE TABLE IF NOT EXISTS storage_cursors (
    session_id TEXT PRIMARY KEY,
    last_epoch_id INTEGER NOT NULL,
    last_frame_sequence INTEGER NOT NULL,
    last_event_sequence INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

-- 5. Connections Dimension Table (Projection)
CREATE TABLE IF NOT EXISTS connections (
    session_id TEXT NOT NULL,
    epoch_id INTEGER NOT NULL,
    connection_id TEXT NOT NULL,
    
    mihomo_start TEXT,
    first_observed_at TEXT NOT NULL,
    last_observed_at TEXT NOT NULL,
    disappeared_observed_at TEXT,
    
    state TEXT NOT NULL CHECK (state IN ('active', 'disappeared_from_snapshot')),
    preexisting_at_start BOOLEAN NOT NULL DEFAULT 0,
    possible_unobserved_tail BOOLEAN NOT NULL DEFAULT 0,
    start_classification TEXT,
    
    process TEXT,
    process_path TEXT,
    host TEXT,
    sniff_host TEXT,
    network TEXT,
    type TEXT,
    source_ip TEXT,
    source_port TEXT,
    destination_ip TEXT,
    remote_destination TEXT,
    destination_port TEXT,
    dns_mode TEXT,
    special_proxy TEXT,
    special_rules_json TEXT,
    inbound_user TEXT,
    inbound_name TEXT,
    inbound_port TEXT,
    
    rule TEXT,
    rule_payload TEXT,
    chains_json TEXT,
    provider_chains_json TEXT,
    
    route TEXT NOT NULL,
    latest_attribution_class TEXT NOT NULL,
    quality_flags_json TEXT NOT NULL,
    relay_evidence_json TEXT,
    
    baseline_upload_counter INTEGER NOT NULL DEFAULT 0,
    baseline_download_counter INTEGER NOT NULL DEFAULT 0,
    last_observed_upload_counter INTEGER NOT NULL DEFAULT 0,
    last_observed_download_counter INTEGER NOT NULL DEFAULT 0,
    monitored_upload_total INTEGER NOT NULL DEFAULT 0,
    monitored_download_total INTEGER NOT NULL DEFAULT 0,
    
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    
    PRIMARY KEY (session_id, epoch_id, connection_id)
);

CREATE INDEX IF NOT EXISTS idx_conn_filter ON connections(first_observed_at, route, process, host);
CREATE INDEX IF NOT EXISTS idx_conn_last_obs ON connections(last_observed_at);

-- 6. Connection Traffic Time Series (Projection)
CREATE TABLE IF NOT EXISTS connection_traffic (
    event_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    epoch_id INTEGER NOT NULL,
    connection_id TEXT NOT NULL,
    
    observed_at TEXT NOT NULL,
    interval_start TEXT,
    interval_end TEXT,
    precision TEXT NOT NULL,
    
    delta_upload INTEGER NOT NULL DEFAULT 0,
    delta_download INTEGER NOT NULL DEFAULT 0,
    
    observed_upload_counter INTEGER NOT NULL DEFAULT 0,
    observed_download_counter INTEGER NOT NULL DEFAULT 0,
    monitored_upload_total INTEGER NOT NULL DEFAULT 0,
    monitored_download_total INTEGER NOT NULL DEFAULT 0,
    
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_traffic_conn_time ON connection_traffic(session_id, epoch_id, connection_id, observed_at);
CREATE INDEX IF NOT EXISTS idx_traffic_obs_at ON connection_traffic(observed_at);

-- 7. Monitoring Gaps Table (Projection)
CREATE TABLE IF NOT EXISTS monitoring_gaps (
    gap_id TEXT PRIMARY KEY,
    source TEXT NOT NULL CHECK (source IN ('controller_stream', 'collector_session_boundary')),
    session_id TEXT,
    open_event_id TEXT,
    close_event_id TEXT,
    
    started_at TEXT NOT NULL,
    ended_at TEXT,
    duration_ms INTEGER,
    
    reason TEXT NOT NULL,
    global_gap_upload_delta INTEGER,
    global_gap_download_delta INTEGER,
    physical_delta_unavailable BOOLEAN NOT NULL DEFAULT 0,
    
    precision TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_gaps_time ON monitoring_gaps(started_at, ended_at);

-- 8. Residual Intervals Table (Projection)
CREATE TABLE IF NOT EXISTS residual_intervals (
    event_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    epoch_id INTEGER NOT NULL,
    observed_at TEXT NOT NULL,
    
    global_upload_delta INTEGER NOT NULL DEFAULT 0,
    global_download_delta INTEGER NOT NULL DEFAULT 0,
    unique_observed_upload INTEGER NOT NULL DEFAULT 0,
    unique_observed_download INTEGER NOT NULL DEFAULT 0,
    residual_upload INTEGER NOT NULL DEFAULT 0,
    residual_download INTEGER NOT NULL DEFAULT 0,
    
    derivation_version TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_residual_obs ON residual_intervals(observed_at);

-- 9. Collector Health Log (Projection)
CREATE TABLE IF NOT EXISTS collector_health (
    event_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    epoch_id INTEGER NOT NULL,
    observed_at TEXT NOT NULL,
    connection_id TEXT,
    issue TEXT NOT NULL,
    details_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_health_obs ON collector_health(observed_at);
