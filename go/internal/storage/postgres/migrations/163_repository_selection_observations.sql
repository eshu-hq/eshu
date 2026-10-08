-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7625: repository_selection_observations records, per git repository scope
-- and per selector, whether the newest complete GitHub org listing still
-- selects the repository. Only shard 0 of a githubOrg collector writes, once
-- per cycle, in one batched upsert ordered by scope_id; a row advances only
-- when the new evaluated_at is later than the stored one, so a replay or a
-- lagging replica is a no-op. selector_id hashes the source mode, the
-- lowercased org, the sorted rules, and the include-archived flag, so two
-- configurations on one org keep separate rows.
--
-- state is evidence only: nothing reads it to delete, hide, or retire a scope.
-- not_listed counts as confirmed only when unlisted_cycle_count >= 2 and
-- evaluated_at - first_unlisted_at >= evaluation_interval_seconds. Rows exist
-- only for scopes already in ingestion_scopes and carry no foreign key, like
-- repository_reindex_requests (162); the writer never locks ingestion_scopes.
--
-- The per-cycle selector read uses the selector_id index: 0.22 ms for 1000
-- rows on PostgreSQL 18, against a sequential scan otherwise.
CREATE TABLE IF NOT EXISTS repository_selection_observations (
    scope_id TEXT NOT NULL,
    selector_id TEXT NOT NULL,
    selector_kind TEXT NOT NULL CHECK (selector_kind IN ('github_org')),
    selector_owner TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('selected', 'archived_excluded', 'rule_excluded', 'not_listed')),
    github_repo_id BIGINT NULL,
    last_listed_at TIMESTAMPTZ NULL,
    first_unlisted_at TIMESTAMPTZ NULL,
    unlisted_cycle_count INTEGER NOT NULL DEFAULT 0,
    evaluated_at TIMESTAMPTZ NOT NULL,
    evaluation_interval_seconds INTEGER NOT NULL,
    PRIMARY KEY (scope_id, selector_id)
);

CREATE INDEX IF NOT EXISTS repository_selection_observations_selector_idx
    ON repository_selection_observations (selector_id);
