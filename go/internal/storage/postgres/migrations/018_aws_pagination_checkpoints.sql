CREATE TABLE IF NOT EXISTS aws_scan_pagination_checkpoints (
    collector_instance_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    region TEXT NOT NULL,
    service_kind TEXT NOT NULL,
    resource_parent TEXT NOT NULL DEFAULT '',
    operation TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    page_token TEXT NOT NULL DEFAULT '',
    page_number INTEGER NOT NULL DEFAULT 0,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (collector_instance_id, account_id, region, service_kind, resource_parent, operation)
);

CREATE INDEX IF NOT EXISTS aws_scan_pagination_checkpoints_scope_idx
    ON aws_scan_pagination_checkpoints (
        collector_instance_id,
        account_id,
        region,
        service_kind,
        generation_id,
        updated_at DESC
    );
