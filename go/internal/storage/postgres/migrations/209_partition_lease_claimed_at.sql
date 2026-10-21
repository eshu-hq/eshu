-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Claim-epoch column for the retirement shared-projection barrier (#8659,
-- Deliverable 2 steps 2b/2b').
--
-- 2b stamps intent_barrier_at after the shared-intent delete commits; 2b'
-- waits until no live lease claimed at or before that barrier remains. The
-- barrier therefore needs each lease's claim epoch: claimed_at is set to
-- clock_timestamp() when a new owner takes the lease and kept across that
-- owner's heartbeats, so the 2b' predicate (claimed_at IS NULL OR
-- claimed_at <= intent_barrier_at) names exactly the leases that could
-- still carry pre-barrier intent state into a graph write. Rows from before
-- this migration have claimed_at NULL and block until their first
-- re-claim, which is one worker cycle.
ALTER TABLE shared_projection_partition_leases
    ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ NULL;
