-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7127 PR-3a: the changed-since link ledger. The reducer's changed_since_link
-- writer (go/internal/storage/postgres/freshness/links) keeps, per scope, the
-- effective key state of the scope's latest linked generation and one link
-- delta per activation, so a later changed-since read is O(changes) instead of
-- a two-generation diff over the whole repository.
--
-- None of these tables carries a foreign key. Every FK child insert takes
-- KEY SHARE on the parent row until commit; a link transaction runs for
-- seconds and would hold KEY SHARE on the ingestion_scopes row, which the
-- projector heartbeat (FOR NO KEY UPDATE SKIP LOCKED) and generation retention
-- (FOR UPDATE SKIP LOCKED) both lock. #7115 deadlocked the projector claim
-- against Ack on exactly that multixact shape. A root build also fired the RI
-- trigger once per state row (771,201 calls in the shim). Rows of a deleted
-- scope are removed by the writer's owner, not by a cascade.
--
-- The writer is dark (ESHU_CHANGED_SINCE_LINK_ENABLED, default off); nothing
-- reads these tables yet.

-- Ordering authority: one row per activation of a generation in a scope.
-- activation_seq is the only ordering the writer trusts; activated_at is
-- informational. The sweeper writes source = 'sweeper' with a NULL prior;
-- the backfill writes source = 'backfill'.
CREATE TABLE IF NOT EXISTS changed_since_activations (
    activation_seq      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    scope_id            TEXT NOT NULL,
    generation_id       TEXT NOT NULL,
    prior_generation_id TEXT NULL,
    source              TEXT NOT NULL,
    activated_at        TIMESTAMPTZ NOT NULL,
    UNIQUE (scope_id, generation_id)
);
CREATE INDEX IF NOT EXISTS changed_since_activations_scope_seq_idx
    ON changed_since_activations (scope_id, activation_seq);

-- Effective state of the scope at changed_since_scope_cursor.state_generation_id:
-- one narrow row per live key. state is the SHA-256 multiset digest of the
-- key's active payload digests.
CREATE TABLE IF NOT EXISTS changed_since_key_state (
    scope_id        TEXT NOT NULL,
    fact_category   TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    fact_kind       TEXT NOT NULL,
    state           BYTEA NOT NULL,
    owner_uri       TEXT NULL,
    PRIMARY KEY (scope_id, fact_category, stable_fact_key)
);
CREATE INDEX IF NOT EXISTS changed_since_key_state_owner_idx
    ON changed_since_key_state (scope_id, owner_uri) WHERE owner_uri IS NOT NULL;

-- Per-scope writer fence and the generation the state table represents.
-- state_generation_id is NULL before the first root and after a
-- digest_version change; a chain break keeps it (#7127 ruling 8.5).
--
-- The attempt columns bound a link that keeps failing (#7127 ruling 8.10).
-- A counting failure of the head activation (attempt_activation_seq) adds one
-- to attempt_count, records last_failure_class, and backs off until
-- next_attempt_at. At the attempt limit the activation becomes a
-- link_poisoned chain break: state_activation_seq advances past it, the state
-- is kept, and poisoned_activation_seq and poisoned_at mark the scope until
-- its next full link. Nothing about attempts is written to
-- changed_since_activations.
CREATE TABLE IF NOT EXISTS changed_since_scope_cursor (
    scope_id                TEXT PRIMARY KEY,
    state_generation_id     TEXT NULL,
    state_activation_seq    BIGINT NOT NULL DEFAULT 0,
    digest_version          SMALLINT NOT NULL,
    updated_at              TIMESTAMPTZ NOT NULL,
    attempt_activation_seq  BIGINT NULL,
    attempt_count           INTEGER NOT NULL DEFAULT 0,
    next_attempt_at         TIMESTAMPTZ NULL,
    last_failure_class      TEXT NULL,
    poisoned_activation_seq BIGINT NULL,
    poisoned_at             TIMESTAMPTZ NULL
);

-- One row per link (older -> newer). prior_generation_id is '' for a root.
CREATE TABLE IF NOT EXISTS changed_since_links (
    scope_id              TEXT NOT NULL,
    generation_id         TEXT NOT NULL,
    prior_generation_id   TEXT NOT NULL,
    link_kind             TEXT NOT NULL,
    digest_version        SMALLINT NOT NULL,
    delta_rows            BIGINT NOT NULL,
    files_keys            BIGINT NOT NULL,
    content_entities_keys BIGINT NOT NULL,
    facts_keys            BIGINT NOT NULL,
    computed_at           TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, prior_generation_id)
);

-- The keys one link changed. fact_category and stable_fact_key stay NOT NULL:
-- the planned chain read uses NOT IN over them, which is equivalent to
-- NOT EXISTS only while neither column can be NULL. The primary key's column
-- order serves the chain read's (scope, generation, prior) equality and the
-- single-link sample order; no other index is intended.
CREATE TABLE IF NOT EXISTS changed_since_link_deltas (
    scope_id            TEXT NOT NULL,
    generation_id       TEXT NOT NULL,
    prior_generation_id TEXT NOT NULL,
    fact_category       TEXT NOT NULL,
    classification      TEXT NOT NULL,
    stable_fact_key     TEXT NOT NULL,
    prior_fact_kind     TEXT NULL,
    current_fact_kind   TEXT NULL,
    prior_state         BYTEA NULL,
    current_state       BYTEA NULL,
    current_tombstoned  BOOLEAN NOT NULL,
    PRIMARY KEY (scope_id, generation_id, prior_generation_id, fact_category, classification, stable_fact_key)
);

CREATE TABLE IF NOT EXISTS changed_since_link_bucket_counts (
    scope_id            TEXT NOT NULL,
    generation_id       TEXT NOT NULL,
    prior_generation_id TEXT NOT NULL,
    fact_category       TEXT NOT NULL,
    classification      TEXT NOT NULL,
    key_count           BIGINT NOT NULL,
    PRIMARY KEY (scope_id, generation_id, prior_generation_id, fact_category, classification)
);
