-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Collector-owned read state of one security-alert org target (#8943).
--
-- One row per org target generation records whether the org stream was cut
-- short and how many allowlisted repositories the fan-out left unread. The
-- unread repositories and their reasons are child rows, not an array: the
-- allowlist has no size bound, and the read path asks about a few repositories
-- by primary key. unread_reasons holds the few distinct reasons so a read with
-- no repository anchor needs only the parent row.
--
-- Both tables are written in the same transaction as their generation, and
-- both cascade from scope_generations, so generation retention removes them
-- with the generation. Neither depends on workflow_work_items.
CREATE TABLE IF NOT EXISTS security_alert_target_read_states (
    generation_id TEXT PRIMARY KEY REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL,
    org_read_truncated BOOLEAN NOT NULL,
    repositories_total INTEGER NOT NULL CHECK (repositories_total >= 0),
    unread_total INTEGER NOT NULL CHECK (unread_total >= 0),
    unread_reasons TEXT[] NOT NULL DEFAULT '{}',
    observed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS security_alert_unread_repositories (
    generation_id TEXT NOT NULL REFERENCES security_alert_target_read_states(generation_id) ON DELETE CASCADE,
    repository TEXT NOT NULL,
    reason TEXT NOT NULL,
    PRIMARY KEY (generation_id, repository)
);
