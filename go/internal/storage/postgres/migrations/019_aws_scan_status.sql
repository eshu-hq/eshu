CREATE TABLE IF NOT EXISTS aws_scan_status (
    collector_instance_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    region TEXT NOT NULL,
    service_kind TEXT NOT NULL,
    scope_id TEXT NOT NULL DEFAULT '',
    generation_id TEXT NOT NULL DEFAULT '',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    commit_status TEXT NOT NULL,
    failure_class TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    api_call_count INTEGER NOT NULL DEFAULT 0,
    throttle_count INTEGER NOT NULL DEFAULT 0,
    warning_count INTEGER NOT NULL DEFAULT 0,
    resource_count INTEGER NOT NULL DEFAULT 0,
    relationship_count INTEGER NOT NULL DEFAULT 0,
    tag_observation_count INTEGER NOT NULL DEFAULT 0,
    budget_exhausted BOOLEAN NOT NULL DEFAULT FALSE,
    credential_failed BOOLEAN NOT NULL DEFAULT FALSE,
    last_started_at TIMESTAMPTZ NULL,
    last_observed_at TIMESTAMPTZ NULL,
    last_completed_at TIMESTAMPTZ NULL,
    last_successful_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (collector_instance_id, account_id, region, service_kind)
);

CREATE INDEX IF NOT EXISTS aws_scan_status_status_idx
    ON aws_scan_status (status, commit_status, updated_at DESC);

CREATE INDEX IF NOT EXISTS aws_scan_status_tuple_updated_idx
    ON aws_scan_status (
        collector_instance_id,
        account_id,
        region,
        service_kind,
        updated_at DESC
    );
