-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7625: repository_selection_observations holds the git collector's
-- per-cycle selection evaluations: for each (scope, selector) pair, whether
-- the scope's repository was in the selector's listing the last time that
-- selector evaluated. A selector is one githubOrg or explicit-list
-- configuration; its id hashes the mode, org, rules, archived setting, and
-- credential identity, so two collectors that list different repositories
-- never share a row. States are selected, archived_excluded, rule_excluded,
-- and not_listed. The row also stores the GitHub repository id so a later
-- phase can detect renames.
--
-- Each evaluation upserts one row per known same-org scope. state_since marks
-- the first evaluation that observed the current state and state_cycle_count
-- how many consecutive evaluations have seen it; an exclusion state counts
-- as confirmed only after two evaluations at least five minutes apart, and a
-- row is live only while evaluated_at plus liveness_window_seconds still
-- covers now. last_listed_at is the last evaluation that saw the scope in
-- the listing (NULL when no evaluation ever has). The table has no foreign
-- key and nothing writes it back to ingestion_scopes: phase 1 observes and
-- reports, it hides, deletes, and retires nothing.
--
-- The per-cycle prior-row read filters on selector_id alone, served by the
-- (selector_id, evaluated_at DESC) index; per-scope freshness reads hit the
-- (scope_id, selector_id) primary key prefix.
CREATE TABLE IF NOT EXISTS repository_selection_observations (
    scope_id TEXT NOT NULL,
    selector_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('selected', 'archived_excluded', 'rule_excluded', 'not_listed')),
    github_repository_id BIGINT NULL,
    evaluated_at TIMESTAMPTZ NOT NULL,
    liveness_window_seconds INTEGER NOT NULL CHECK (liveness_window_seconds > 0),
    state_since TIMESTAMPTZ NOT NULL,
    state_cycle_count INTEGER NOT NULL CHECK (state_cycle_count >= 1),
    last_listed_at TIMESTAMPTZ NULL,
    PRIMARY KEY (scope_id, selector_id)
);

CREATE INDEX IF NOT EXISTS repository_selection_observations_selector_evaluated_idx
    ON repository_selection_observations (selector_id, evaluated_at DESC);
