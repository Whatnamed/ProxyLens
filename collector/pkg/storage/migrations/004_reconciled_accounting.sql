-- 004_reconciled_accounting.sql: Tables for versioned reconciled accounting & conservative relay deduplication

-- 1. Accounting Runs Table
CREATE TABLE IF NOT EXISTS accounting_runs (
    run_id TEXT PRIMARY KEY,
    algorithm_version TEXT NOT NULL,
    started_at TEXT NOT NULL,
    completed_at TEXT,
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    source_journal_event_count INTEGER NOT NULL,
    source_boundary_json TEXT NOT NULL,
    notes TEXT
);

-- 2. Relay Relations Table
CREATE TABLE IF NOT EXISTS relay_relations (
    run_id TEXT NOT NULL,
    candidate_session_id TEXT NOT NULL,
    candidate_epoch_id INTEGER NOT NULL,
    candidate_connection_id TEXT NOT NULL,
    logical_session_id TEXT,
    logical_epoch_id INTEGER,
    logical_connection_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('confirmed', 'ambiguous', 'unpaired')),
    evidence_json TEXT NOT NULL,
    derivation_version TEXT NOT NULL,
    PRIMARY KEY (run_id, candidate_session_id, candidate_epoch_id, candidate_connection_id)
);

-- 3. Accounted Traffic Table (One-to-one derived row per raw connection_traffic event)
CREATE TABLE IF NOT EXISTS accounted_traffic (
    run_id TEXT NOT NULL,
    source_event_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    epoch_id INTEGER NOT NULL,
    connection_id TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    interval_start TEXT,
    interval_end TEXT,
    precision TEXT NOT NULL,
    route TEXT NOT NULL,
    raw_upload INTEGER NOT NULL,
    raw_download INTEGER NOT NULL,
    accounted_upload INTEGER NOT NULL,
    accounted_download INTEGER NOT NULL,
    accounting_class TEXT NOT NULL CHECK (accounting_class IN ('unique', 'confirmed_relay_duplicate', 'ambiguous_relay', 'missing_attribution')),
    process TEXT,
    process_path TEXT,
    host TEXT,
    sniff_host TEXT,
    destination_ip TEXT,
    network TEXT,
    rule TEXT,
    rule_payload TEXT,
    final_proxy TEXT,
    top_policy_group TEXT,
    dimension_derivation_version TEXT NOT NULL,
    PRIMARY KEY (run_id, source_event_id)
);

CREATE INDEX IF NOT EXISTS idx_acc_traffic_obs ON accounted_traffic(run_id, observed_at);
CREATE INDEX IF NOT EXISTS idx_acc_traffic_route ON accounted_traffic(run_id, route, observed_at);
CREATE INDEX IF NOT EXISTS idx_acc_traffic_process ON accounted_traffic(run_id, process, observed_at);
CREATE INDEX IF NOT EXISTS idx_acc_traffic_host ON accounted_traffic(run_id, host, observed_at);
CREATE INDEX IF NOT EXISTS idx_acc_traffic_final_proxy ON accounted_traffic(run_id, final_proxy, observed_at);
