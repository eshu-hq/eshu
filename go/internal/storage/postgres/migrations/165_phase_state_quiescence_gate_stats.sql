-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7166: teach the planner that scope_id and generation_id are correlated
-- in graph_projection_phase_state, so the canonical-code quiescence gate
-- probe keeps its primary-key pushdown.
--
-- The gate (HasUncommittedCanonicalCodeScopes) probes the phase table per
-- repository fact with (scope_id, generation_id, keyspace, phase,
-- acceptance_unit_id). Without dependency statistics the planner multiplies
-- the scope and generation selectivities and estimates rows=1 for the
-- four-column probe while 75 to 150 rows actually match; at that estimate it
-- prefers migration-156's single-column generation_idx (chosen for the
-- retention cascade, which must keep it) and demotes acceptance_unit_id to
-- a join filter, detoasting payload->>'repo_id' ~9M times per evaluation.
--
-- Measured on PostgreSQL 18, production DDL, 619,320 facts / 794 scopes /
-- 119,100 phases, VACUUM ANALYZEd, custom plans, three back-to-back runs
-- each (docs/internal/evidence/7166-quiescence-gate-stats.md): gate text
-- unchanged, 4,871 ms / 726,673 buffers before, 427 ms / 603,253 buffers
-- after, with the pkey carrying all five quals at rows=1.00 exact. An
-- ndistinct object on the same columns moves nothing; dependencies is the
-- operative kind. A query rewrite flattening the gate to anti-joins was
-- measured and rejected: it is immune to these statistics (3.2 s /
-- 9.36M buffers with and without) and flips between parallel and serial
-- plans across identical executions.
--
-- Transfer caveats, all recorded in the evidence note: the shim holds ~150
-- phases per generation against ops-qa's ~9 (migration 156's census:
-- 91,558 rows / 10,167 generations), and its scope->generation dependency
-- (1.0 both directions) may be partly shim-only; the generation->scope
-- direction holds universally by foreign key, which is the direction that
-- repairs this probe's estimate. Production effect is judged from the new
-- eshu_dp_shared_projection_lane_gate_seconds histogram, not from the shim.
--
-- ANALYZE populates the new statistics immediately so the first gate probe
-- after bootstrap plans correctly; autovacuum keeps them current afterwards.
-- IF NOT EXISTS for rerun safety.
CREATE STATISTICS IF NOT EXISTS graph_projection_phase_state_scope_generation_stats
    (dependencies) ON scope_id, generation_id
    FROM graph_projection_phase_state;
ANALYZE graph_projection_phase_state;
