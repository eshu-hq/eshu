
CREATE TABLE IF NOT EXISTS runtime_ingester_control (
    ingester TEXT PRIMARY KEY,
    scan_request_status TEXT NOT NULL DEFAULT 'idle',
    scan_request_requested_at TIMESTAMPTZ,
    scan_request_claimed_at TIMESTAMPTZ,
    scan_request_completed_at TIMESTAMPTZ,
    scan_request_error TEXT,
    reindex_request_status TEXT NOT NULL DEFAULT 'idle',
    reindex_request_requested_at TIMESTAMPTZ,
    reindex_request_claimed_at TIMESTAMPTZ,
    reindex_request_completed_at TIMESTAMPTZ,
    reindex_request_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
