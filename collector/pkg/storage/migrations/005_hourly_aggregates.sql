-- 005_hourly_aggregates.sql: Table for materialized hourly dimension aggregates

CREATE TABLE IF NOT EXISTS usage_hourly_dimensions (
    run_id TEXT NOT NULL,
    bucket_start TEXT NOT NULL,
    dimension_type TEXT NOT NULL,
    dimension_key TEXT NOT NULL,
    route TEXT NOT NULL,
    upload_bytes INTEGER NOT NULL,
    download_bytes INTEGER NOT NULL,
    connection_count INTEGER NOT NULL,
    exact_upload_bytes INTEGER NOT NULL,
    exact_download_bytes INTEGER NOT NULL,
    estimated_upload_bytes INTEGER NOT NULL,
    estimated_download_bytes INTEGER NOT NULL,
    PRIMARY KEY (
        run_id,
        bucket_start,
        dimension_type,
        dimension_key,
        route
    )
);

CREATE INDEX IF NOT EXISTS idx_hourly_dim ON usage_hourly_dimensions(run_id, dimension_type, bucket_start);
CREATE INDEX IF NOT EXISTS idx_hourly_route ON usage_hourly_dimensions(run_id, route, bucket_start);
