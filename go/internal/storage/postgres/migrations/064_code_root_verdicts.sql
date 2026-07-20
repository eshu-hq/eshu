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
