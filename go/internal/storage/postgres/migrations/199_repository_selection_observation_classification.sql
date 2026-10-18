-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8454: store the upstream classification probe verdict on
-- repository_selection_observations. A Confirmed not_listed scope is probed
-- by stored github_repo_id (GET /repositories/{id}, /repos/{full_name}
-- fallback when the id is 0); the verdict lands here, never in
-- ingestion_scopes, facts, or the graph.
--
-- upstream_classification is NULL until a probe reports. It never reads
-- "deleted": a 404 is not_found_or_inaccessible, which also covers a
-- private repository the listing credential cannot see. successor_slug
-- carries the probe's full_name for renamed and transferred verdicts;
-- whether it resolves to a selected scope is decided at read time only.
-- classified_at is the probe time and gates the re-probe: a scope
-- re-probes at most once per liveness window.
--
-- The evaluation upsert (migration 166, extended here only by column reads)
-- clears all three columns whenever it writes any other state: a verdict
-- belongs to a not_listed row, so a relisted scope never carries a stale
-- one. Like every migration in this directory, this file runs on EVERY
-- bootstrap (there is no applied-migration ledger), so every statement is
-- idempotent: ADD COLUMN IF NOT EXISTS plus a guarded CHECK add that is a
-- no-op once applied.
ALTER TABLE repository_selection_observations
    ADD COLUMN IF NOT EXISTS upstream_classification TEXT NULL;
ALTER TABLE repository_selection_observations
    ADD COLUMN IF NOT EXISTS successor_slug TEXT NULL;
ALTER TABLE repository_selection_observations
    ADD COLUMN IF NOT EXISTS classified_at TIMESTAMPTZ NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'repository_selection_observations_upstream_classification_check'
          AND conrelid = 'repository_selection_observations'::regclass
    ) THEN
        ALTER TABLE repository_selection_observations
            ADD CONSTRAINT repository_selection_observations_upstream_classification_check
            CHECK (upstream_classification IN ('transferred', 'renamed', 'still_present', 'not_found_or_inaccessible'));
    END IF;
END
$$;
