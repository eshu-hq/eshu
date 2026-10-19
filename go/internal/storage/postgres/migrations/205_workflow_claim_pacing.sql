-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Claim-pacing latch state (#8470).
--
-- Depth pacing delays claim grants on per-domain pending depth first, with
-- global pending depth as the ceiling, under one hysteresis latch per
-- (signal, domain). Between the low and high watermarks the correct verdict
-- depends on whether pacing was already engaged, so that bit is state, not
-- a pure function of current depth. With more than one coordinator it must
-- live in shared storage alongside the claim path, not in coordinator
-- memory -- otherwise two coordinators disagree about whether pacing is on.
--
-- Rows appear only after a first engagement (a missing row reads as
-- disengaged), so no backfill is needed. Latch advances are single-row
-- upserts keyed by (signal, domain): two coordinators racing an engagement
-- or a release converge on the same row, never on conflicting state.
CREATE TABLE IF NOT EXISTS workflow_claim_pacing (
    signal TEXT NOT NULL,
    domain TEXT NOT NULL,
    engaged BOOLEAN NOT NULL DEFAULT FALSE,
    engaged_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (signal, domain)
);
