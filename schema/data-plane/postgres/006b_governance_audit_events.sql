CREATE TABLE IF NOT EXISTS governance_audit_events (
    event_id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    actor_class TEXT NOT NULL,
    actor_id_hash TEXT NULL,
    service_principal_id TEXT NULL,
    scope_class TEXT NOT NULL,
    scope_id_hash TEXT NULL,
    decision TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    correlation_id TEXT NULL,
    policy_revision_hash TEXT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    persisted_at TIMESTAMPTZ NOT NULL,
    tenant_id TEXT NULL,
    workspace_id TEXT NULL
);

ALTER TABLE governance_audit_events ADD COLUMN IF NOT EXISTS tenant_id TEXT NULL;
ALTER TABLE governance_audit_events ADD COLUMN IF NOT EXISTS workspace_id TEXT NULL;

CREATE INDEX IF NOT EXISTS governance_audit_events_query_idx
    ON governance_audit_events (
        event_type,
        actor_class,
        scope_class,
        decision,
        occurred_at ASC,
        event_id ASC
    );

CREATE INDEX IF NOT EXISTS governance_audit_events_correlation_idx
    ON governance_audit_events (correlation_id, occurred_at ASC, event_id ASC)
    WHERE correlation_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS governance_audit_events_reason_idx
    ON governance_audit_events (reason_code, occurred_at ASC, event_id ASC);

CREATE INDEX IF NOT EXISTS governance_audit_events_tenant_idx
    ON governance_audit_events (tenant_id, occurred_at ASC, event_id ASC)
    WHERE tenant_id IS NOT NULL;
