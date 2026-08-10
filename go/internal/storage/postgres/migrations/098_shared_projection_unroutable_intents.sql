-- Durable record of shared-projection intents a canonical edge write could not
-- route (#5984). A sibling of reducer_input_invalid_facts rather than a reuse:
-- that table is keyed on a fact identity an intent row does not have.
--
-- intent_id alone is the primary key because BuildSharedProjectionIntent
-- derives it as a hash over scope, generation, domain, partition key,
-- repository and source run, so it already encodes the natural key.
--
-- No foreign keys, deliberately: shared_projection_intents.scope_id is
-- TEXT NOT NULL DEFAULT '' (migration 008), so a legacy row can carry an empty
-- scope and an FK to ingestion_scopes would reject the insert exactly when a
-- malformed row is being recorded. Generation retention reaps these rows with
-- an explicit statement instead.
CREATE TABLE IF NOT EXISTS shared_projection_unroutable_intents (
    intent_id TEXT PRIMARY KEY,
    projection_domain TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    evidence_source TEXT NOT NULL,
    reason TEXT NOT NULL,
    decided_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS shared_projection_unroutable_intents_scope_generation_idx
    ON shared_projection_unroutable_intents (scope_id, generation_id, projection_domain, decided_at DESC);
