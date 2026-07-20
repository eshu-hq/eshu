-- #5376: reducer-owned repo-wide Rails-controller dead-code-root verdicts.
--
-- The Ruby parser roots a *Controller action from a SAME-FILE superclass walk;
-- a controller whose real base lives in another file (or a reopened class) is
-- over-kept. This table stores the repo-wide verdict the CodeReachability
-- projection runner computes in its existing partition transaction. The
-- dead-code query acts ONLY on 'downgraded' rows; absence of a row means the
-- reducer proved nothing and the parser's root is kept (lag-safety keystone).
-- The table is deliberately kind-generic so other guess-based framework roots
-- can reuse it later.
CREATE TABLE IF NOT EXISTS code_root_verdicts (
    scope_id      TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    repository_id TEXT NOT NULL,
    entity_id     TEXT NOT NULL,
    root_kind     TEXT NOT NULL,
    verdict       TEXT NOT NULL,
    basis         JSONB NOT NULL,
    observed_at   TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, repository_id, entity_id, root_kind)
);

CREATE INDEX IF NOT EXISTS code_root_verdicts_repo_entity_verdict_idx
    ON code_root_verdicts (repository_id, entity_id, verdict);

-- Upgrade-backfill epoch (Option C). On an upgraded deployment the verdicts
-- table is created empty but code_reachability_repository_watermarks already
-- carries a row per already-indexed repo, and the loader schedules a repo only
-- when a completed code intent is newer than that watermark. Without a nudge,
-- BuildCodeRootVerdicts never runs for existing repos and #5376 silently does
-- nothing until a re-index. This column records the verdict schema epoch the
-- watermark was projected under; pre-upgrade rows keep the DEFAULT 0 ("projected
-- before verdicts existed"), so the loader re-schedules each such repo exactly
-- once until the runner re-stamps it with the current epoch. Idempotent under
-- re-execution (schema.go re-runs migrations on every boot; this repo has no
-- migration version ledger), mirroring the `truncated` column in
-- 027_code_reachability.sql.
ALTER TABLE code_reachability_repository_watermarks
    ADD COLUMN IF NOT EXISTS verdict_schema_epoch INTEGER NOT NULL DEFAULT 0;
