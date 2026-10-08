-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7625: repository_selection_observations records, per git repository scope
-- and per selector, whether the selector's newest evaluation still selects the
-- repository. Only shard 0 of a git collector writes, once per cycle, in one
-- batched upsert ordered by scope_id: a githubOrg selector writes every known
-- scope of its org (selected, excluded, or not_listed), an explicit selector
-- writes only selected rows for its configured repositories. A row advances
-- only when the new evaluated_at is later than the stored one, so a replay or
-- a lagging replica is a no-op. selector_id is a hash of the selector kind,
-- source mode, lowercased owner, normalized sorted rules, and include-archived
-- flag, followed by "@" and the listing credential's principal, so two
-- configurations or credentials on one owner keep separate rows.
--
-- state is evidence only: nothing reads it to delete, hide, or retire a scope.
-- state_since is when the row entered state and state_cycle_count is how many
-- evaluations in a row wrote it: a repeated state keeps state_since and adds
-- one, a changed state restarts both. A row is live while evaluated_at +
-- liveness_window_seconds >= now; an excluded row is confirmed when
-- state_cycle_count >= 2 and evaluated_at - state_since >= 5 minutes. Rows
-- exist only for scopes already in ingestion_scopes and carry no foreign key,
-- like repository_reindex_requests (162); the writer never locks
-- ingestion_scopes.
--
-- The per-cycle selector read uses the selector_id index: 0.20 ms for 1000
-- rows on PostgreSQL 18, against a sequential scan otherwise. The freshness
-- read by scope_id uses the primary key.
CREATE TABLE IF NOT EXISTS repository_selection_observations (
    scope_id TEXT NOT NULL,
    selector_id TEXT NOT NULL,
    selector_kind TEXT NOT NULL CHECK (selector_kind IN ('github_org', 'explicit')),
    selector_owner TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('selected', 'archived_excluded', 'rule_excluded', 'not_listed')),
    github_repo_id BIGINT NULL,
    last_listed_at TIMESTAMPTZ NULL,
    state_since TIMESTAMPTZ NOT NULL,
    state_cycle_count INTEGER NOT NULL CHECK (state_cycle_count > 0),
    evaluated_at TIMESTAMPTZ NOT NULL,
    liveness_window_seconds INTEGER NOT NULL CHECK (liveness_window_seconds > 0),
    PRIMARY KEY (scope_id, selector_id)
);

CREATE INDEX IF NOT EXISTS repository_selection_observations_selector_idx
    ON repository_selection_observations (selector_id);
