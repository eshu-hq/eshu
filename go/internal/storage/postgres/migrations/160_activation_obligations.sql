-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7584: durable exact-generation activation obligations. ProjectorQueue.Ack
-- inserts one row for the scope generation it activates, inside the Ack
-- transaction, so the obligation exists exactly when the activation commits
-- and never for a rejected, stale, duplicate or superseded Ack. A leased
-- consumer in the resolution engine claims rows (FOR NO KEY UPDATE SKIP
-- LOCKED, lease owner plus a monotonic claim_token), runs the activation
-- maintenance behind its port, wakes the exact deployment_mapping rows that
-- wait on the generation's backward-evidence phase, and completes the row
-- only under a token fence. See go/internal/storage/postgres/activation.
--
-- Primary key order is (generation_id, scope_id). generation_id is globally
-- unique (scope_generations primary key), and the retention prune deletes
-- scope_generations by generation_id, which fires this table's ON DELETE
-- CASCADE once per deleted generation. A generation-leading key lets that
-- cascade probe seek instead of scanning the table (#7419). Equality lookups
-- by (scope_id, generation_id) and ON CONFLICT (scope_id, generation_id) use
-- the same unique index regardless of column order.
--
-- The single foreign key cascades from scope_generations, matching every
-- other generation-owned table; a scope delete reaches this table through
-- its generations. There is deliberately no foreign key to fact_work_items:
-- work_item_id records which projector work item activated the generation,
-- and a second cascade would add a probe per deleted work item for no
-- integrity gain.
--
-- States: pending and leased are open; completed (phase published, waiting
-- rows woken), obsolete (the scope moved to another generation) and
-- inapplicable (the generation can never carry a phase: no repository fact,
-- or no repository maps to it in the shipped read) are terminal. The prune
-- deletes only completed and obsolete rows; an inapplicable row stays until
-- its generation's cascade, so catch-up cannot owe it again.
CREATE TABLE IF NOT EXISTS activation_obligations (
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL,
    work_item_id TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    lease_owner TEXT NULL,
    lease_until TIMESTAMPTZ NULL,
    claim_token BIGINT NOT NULL DEFAULT 0,
    finished_at TIMESTAMPTZ NULL,
    PRIMARY KEY (generation_id, scope_id),
    CONSTRAINT activation_obligations_state_check
        CHECK (state IN ('pending', 'leased', 'completed', 'obsolete', 'inapplicable')),
    CONSTRAINT activation_obligations_lease_check CHECK (
        (state = 'leased' AND lease_owner IS NOT NULL AND lease_until IS NOT NULL)
        OR (state <> 'leased' AND lease_owner IS NULL AND lease_until IS NULL)
    ),
    CONSTRAINT activation_obligations_finished_check CHECK (
        (state IN ('completed', 'obsolete', 'inapplicable')) = (finished_at IS NOT NULL)
    )
);

-- Claim order: oldest open obligation first. Partial, so finished rows that
-- wait for the prune never enter the claim scan.
CREATE INDEX IF NOT EXISTS activation_obligations_open_idx
    ON activation_obligations (created_at, scope_id, generation_id)
    WHERE state IN ('pending', 'leased');

-- Prune order: oldest finished obligation first, bounded per pass. Excludes
-- inapplicable on purpose (see the state comment above).
CREATE INDEX IF NOT EXISTS activation_obligations_finished_idx
    ON activation_obligations (finished_at)
    WHERE state IN ('completed', 'obsolete');
