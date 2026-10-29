-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9299: source-bound, tenant-partitioned durable running-image scans.
-- Scanner image identity is stable across work-item retention and policy changes.
-- Registry credentials and local artifact paths are never stored here.
CREATE TABLE IF NOT EXISTS scanner_image_targets (
    scan_scope_id TEXT PRIMARY KEY,
    authorization_namespace TEXT NOT NULL,
    tenant_id TEXT NOT NULL DEFAULT '',
    workspace_id TEXT NOT NULL DEFAULT '',
    subject_class TEXT NOT NULL DEFAULT '',
    authorization_domain TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    completed_at TIMESTAMPTZ NULL,
    CONSTRAINT scanner_image_targets_namespace_check
      CHECK (authorization_namespace IN ('local', 'hosted')),
    CONSTRAINT scanner_image_targets_boundary_digest_key
      UNIQUE (authorization_namespace, tenant_id, workspace_id, subject_class,
              authorization_domain, manifest_digest)
);

-- Each row is a historical validation of one exact running source fact and
-- registry policy. New policy/configuration revisions add rows rather than
-- overwriting provenance used by prior attempts.
CREATE TABLE IF NOT EXISTS scanner_image_target_sources (
    binding_id TEXT PRIMARY KEY,
    scan_scope_id TEXT NOT NULL REFERENCES scanner_image_targets(scan_scope_id),
    source_fact_id TEXT NOT NULL,
    source_collector_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    source_generation_id TEXT NOT NULL,
    source_payload_sha256 TEXT NOT NULL,
    scanner_collector_instance_id TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL DEFAULT '',
    registry TEXT NOT NULL,
    repository TEXT NOT NULL,
    runtime_digest TEXT NOT NULL,
    runtime_digest_kind TEXT NOT NULL,
    runtime_image_reference TEXT NOT NULL,
    manifest_evidence_fact_id TEXT NOT NULL DEFAULT '',
    manifest_evidence_scope_id TEXT NOT NULL DEFAULT '',
    manifest_evidence_generation_id TEXT NOT NULL DEFAULT '',
    manifest_evidence_payload_sha256 TEXT NOT NULL DEFAULT '',
    registry_domain_config_sha256 TEXT NOT NULL,
    validated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS scanner_image_target_sources_scope_idx
    ON scanner_image_target_sources (scan_scope_id, source_scope_id);

-- Work rows may be retained/deleted; this record keeps the pinned binding and
-- terminal outcome so a failed attempt never poisons the completed ledger.
CREATE TABLE IF NOT EXISTS scanner_image_target_attempts (
    work_item_id TEXT PRIMARY KEY,
    scan_scope_id TEXT NOT NULL REFERENCES scanner_image_targets(scan_scope_id),
    binding_id TEXT NOT NULL REFERENCES scanner_image_target_sources(binding_id),
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS scanner_image_target_attempts_scope_idx
    ON scanner_image_target_attempts (scan_scope_id, created_at DESC);

CREATE OR REPLACE FUNCTION mark_scanner_image_target_attempt() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_scope TEXT;
BEGIN
    UPDATE scanner_image_target_attempts
    SET status = NEW.status,
        finished_at = CASE WHEN NEW.status IN ('completed', 'failed_terminal', 'expired')
                           THEN NEW.updated_at ELSE NULL END
    WHERE work_item_id = NEW.work_item_id
    RETURNING scan_scope_id INTO target_scope;
    IF NEW.status = 'completed' AND target_scope IS NOT NULL THEN
        UPDATE scanner_image_targets
        SET completed_at = COALESCE(NEW.last_completed_at, NEW.updated_at)
        WHERE scan_scope_id = target_scope AND completed_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE TRIGGER workflow_scanner_image_attempt_state
AFTER UPDATE OF status ON workflow_work_items
FOR EACH ROW
WHEN (NEW.collector_kind = 'scanner_worker' AND OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION mark_scanner_image_target_attempt();

ALTER TABLE workflow_work_items
    ADD COLUMN IF NOT EXISTS authorization_scope_id TEXT NOT NULL DEFAULT '';
