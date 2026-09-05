-- 008: Incremental Accounting v2 (Phase 3S)
--
-- Additive migration introducing generation-based incremental accounting.
-- Legacy tables (accounting_runs, accounted_traffic, relay_relations,
-- usage_hourly_dimensions) are preserved untouched: the Query layer falls
-- back to the latest completed legacy run until an active v2 generation
-- exists, and no legacy row is ever deleted by this phase.
--
-- Core invariants:
--   * at most one active generation
--   * the generation's published_journal_sequence only advances inside the
--     same transaction that makes its derived state query-consistent
--   * one canonical derived row per nonzero-byte source event within the
--     active generation (zero-byte raw rows are never deleted, they are
--     simply not duplicated into derived storage - documented compaction)

CREATE TABLE accounting_generations (
    generation_id TEXT PRIMARY KEY,
    algorithm_version TEXT NOT NULL,
    derivation_version TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('seeding', 'materializing', 'active', 'failed', 'superseded')),
    seed_last_sequence INTEGER NOT NULL DEFAULT 0,
    seed_boundary_sequence INTEGER NOT NULL DEFAULT 0,
    materialize_last_sequence INTEGER NOT NULL DEFAULT 0,
    published_journal_sequence INTEGER NOT NULL DEFAULT 0,
    published_frame_time TEXT,
    published_raw_upload INTEGER NOT NULL DEFAULT 0,
    published_raw_download INTEGER NOT NULL DEFAULT 0,
    published_accounted_upload INTEGER NOT NULL DEFAULT 0,
    published_accounted_download INTEGER NOT NULL DEFAULT 0,
    notes TEXT,
    created_at TEXT NOT NULL,
    activated_at TEXT,
    superseded_at TEXT
);

CREATE INDEX idx_accounting_generations_status ON accounting_generations (status);

CREATE TABLE accounting_runs_v2 (
    run_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('seed', 'incremental', 'repair')),
    from_sequence_exclusive INTEGER NOT NULL,
    to_sequence_inclusive INTEGER NOT NULL,
    started_at TEXT NOT NULL,
    completed_at TEXT,
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    processed_events INTEGER NOT NULL DEFAULT 0,
    failed_reason TEXT
);

CREATE INDEX idx_accounting_runs_v2_generation ON accounting_runs_v2 (generation_id, started_at);

CREATE TABLE accounting_conn_state_v2 (
    generation_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    epoch_id INTEGER NOT NULL,
    connection_id TEXT NOT NULL,
    first_observed_at TEXT NOT NULL,
    last_event_at TEXT NOT NULL,
    disappeared_at TEXT,
    route TEXT,
    attribution_class TEXT,
    process TEXT,
    host TEXT,
    destination_ip TEXT,
    rule TEXT,
    rule_payload TEXT,
    chains_json TEXT,
    monitored_upload INTEGER NOT NULL DEFAULT 0,
    monitored_download INTEGER NOT NULL DEFAULT 0,
    accounting_class TEXT NOT NULL DEFAULT 'unique',
    classified_at TEXT,
    PRIMARY KEY (generation_id, session_id, epoch_id, connection_id)
);

CREATE INDEX idx_conn_state_v2_group ON accounting_conn_state_v2 (generation_id, session_id, epoch_id);

CREATE TABLE accounted_traffic_v2 (
    generation_id TEXT NOT NULL,
    source_event_id TEXT NOT NULL,
    source_journal_sequence INTEGER NOT NULL,
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
    PRIMARY KEY (generation_id, source_event_id)
);

CREATE INDEX idx_acc_traffic_v2_conn ON accounted_traffic_v2 (generation_id, session_id, epoch_id, connection_id);
CREATE INDEX idx_acc_traffic_v2_seq ON accounted_traffic_v2 (generation_id, source_journal_sequence);

CREATE TABLE relay_relations_v2 (
    generation_id TEXT NOT NULL,
    candidate_session_id TEXT NOT NULL,
    candidate_epoch_id INTEGER NOT NULL,
    candidate_connection_id TEXT NOT NULL,
    logical_session_id TEXT,
    logical_epoch_id INTEGER,
    logical_connection_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('confirmed', 'ambiguous', 'unpaired')),
    evidence_json TEXT NOT NULL,
    derivation_version TEXT NOT NULL,
    PRIMARY KEY (generation_id, candidate_session_id, candidate_epoch_id, candidate_connection_id)
);

CREATE TABLE usage_hourly_dimensions_v2 (
    generation_id TEXT NOT NULL,
    bucket_start TEXT NOT NULL,
    dimension_type TEXT NOT NULL,
    dimension_key TEXT NOT NULL,
    route TEXT NOT NULL,
    upload_bytes INTEGER NOT NULL DEFAULT 0,
    download_bytes INTEGER NOT NULL DEFAULT 0,
    connection_count INTEGER NOT NULL DEFAULT 0,
    exact_upload_bytes INTEGER NOT NULL DEFAULT 0,
    exact_download_bytes INTEGER NOT NULL DEFAULT 0,
    estimated_upload_bytes INTEGER NOT NULL DEFAULT 0,
    estimated_download_bytes INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (generation_id, bucket_start, dimension_type, dimension_key, route)
);

-- Durable distinct-connection evidence backing usage_hourly_dimensions_v2
-- connection_count under incremental updates. Only connections contributing
-- nonzero accounted bytes to a key are tracked.
CREATE TABLE usage_hourly_dimension_conns_v2 (
    generation_id TEXT NOT NULL,
    bucket_start TEXT NOT NULL,
    dimension_type TEXT NOT NULL,
    dimension_key TEXT NOT NULL,
    route TEXT NOT NULL,
    session_id TEXT NOT NULL,
    epoch_id INTEGER NOT NULL,
    connection_id TEXT NOT NULL,
    PRIMARY KEY (generation_id, bucket_start, dimension_type, dimension_key, route, session_id, epoch_id, connection_id)
);
