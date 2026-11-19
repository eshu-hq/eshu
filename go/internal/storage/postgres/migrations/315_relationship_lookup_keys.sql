-- 315_relationship_lookup_keys.sql: extend the relationship reference candidate
-- sidecar over the two text-search arms of the deferred cross-repository
-- load and add the security-alert reconciliation key sidecar (issue #10875,
-- part of #10860, decided by the #10861 D1/D2 ruling).
--
-- relationship_reference_candidate_keys gains one row per eligible fact
-- (keys_version), the fact kind, the lowercased artifact/path extracts the
-- family predicate is recomposed from in SQL, the two content-marker
-- conjunction results, the Flux presence bit, and the five unconditional
-- payload-marker bits. Every value derives from the fact's own envelope
-- alone (#10861 D2): catalog aliases, repo_ids, and the marker list stay
-- query-time inputs, never stored, so a catalog change needs no rebuild.
--
-- security_alert_reconciliation_keys is the new exploded sidecar for the
-- six-arm security-alert reconciliation load: (fact_id, key_kind,
-- key_value) with verbatim values (no trim, no lowercase — the legacy
-- query compares exact). No fact_records foreign key (explicit retention
-- deletes, #7420); the partition index carries the deletes.

ALTER TABLE relationship_reference_candidate_keys
    ADD COLUMN IF NOT EXISTS keys_version SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS fact_kind TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS artifact_type_norm TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS path_norm TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS content_argocd_kind BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS content_salt_gitfs BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS flux_git_nonempty BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS um_kind_application BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS um_artifact_argocd BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS um_argocd_applications BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS um_argocd_applicationsets BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS um_flux_git_repositories BOOLEAN NOT NULL DEFAULT FALSE;

-- Existing rows predate the per-row version contract: keys_version stays 0
-- so the reader keeps the legacy text-search arms for them until the
-- backfill rewrites each row at version 1.

CREATE TABLE IF NOT EXISTS security_alert_reconciliation_keys (
    fact_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    key_kind TEXT NOT NULL,
    key_value TEXT NOT NULL,
    PRIMARY KEY (fact_id, key_kind, key_value)
);

CREATE INDEX IF NOT EXISTS security_alert_reconciliation_keys_partition_idx
    ON security_alert_reconciliation_keys (scope_id, generation_id, fact_id);

CREATE INDEX IF NOT EXISTS security_alert_reconciliation_keys_lookup_idx
    ON security_alert_reconciliation_keys (key_kind, key_value, scope_id, generation_id, fact_id);

-- Ready marker for the keyed security-alert reader: the reader uses the
-- sidecar only once this marker is present (the
-- PackageManifestConsumptionKeysReady analog). The backfill inserts the
-- marker after the bounded resumable rebuild completes.
CREATE TABLE IF NOT EXISTS security_alert_reconciliation_key_backfill_markers (
    marker_name TEXT PRIMARY KEY,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
