-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Per-component generation-quota usage windows (#8471).
--
-- One row per (component, scope, hour window) counts claim grants issued
-- inside that window. The claim statement reserves a grant by upserting this
-- row in the same statement that grants the claim, so concurrent replicas
-- serialize on the usage row and cannot overshoot: check-then-grant across
-- statements is the bug this table exists to prevent.
--
-- The window is a database-side hour bucket (date_trunc('hour', now())
-- inside the claim statement), never coordinator wall clock, so HA
-- coordinators with skewed clocks share one window. Rows for elapsed
-- windows are pruned by the claim statement itself; no background job owns
-- them.
CREATE TABLE IF NOT EXISTS component_quota_windows (
    component TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    grants INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (component, scope_id, window_start)
);
