-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7419: graph_projection_phase_repair_queue carries generation_id only as
-- the fourth column of its primary key
-- (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace,
-- phase), so the per-prune probes on generation_id cannot seek. Every
-- generation prune (deleteScopeGenerationsForRetentionQuery, DELETE FROM
-- scope_generations WHERE generation_id = ANY($1)) checks this table through
-- its generation_id REFERENCES ... ON DELETE CASCADE foreign key, and the
-- retention row-count query probes it by candidate.generation_id; both read
-- the whole table per pruned generation. On a local fixture (50k rows, 200
-- generations) the probe is a 717-buffer seq scan at ~4.6 ms; with this index
-- it is an index-only seek at ~0.7-0.9 ms warm. See
-- docs/internal/evidence/7419-generation-prune-generation-idx.md.
--
-- Plain (generation_id): the probes are equality matches with no ordering,
-- so a single-column btree is the whole contract.
--
-- Built on cold bootstrap too: nothing rebuilds a deferred index later, and an
-- empty table builds instantly. Keep this the only statement in the file.
-- PostgreSQL rejects a concurrent build inside a transaction block, the
-- migration coordinator runs a sole concurrent definition in autocommit mode
-- without the bootstrap lock_timeout (#7004), and it drops an invalid
-- same-name index before retrying a failed build.
CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_projection_phase_repair_queue_generation_idx
    ON graph_projection_phase_repair_queue (generation_id);
