-- #6785: publish the vacuous canonical-nodes phase for the value-flow
-- refresh singleton anchor.
--
-- Migration 115 seeds scope `eshu:global` with active generation
-- `eshu:global:genesis` and no fact_records. The canonical-code quiescence
-- gate (uncommittedCanonicalCodeScopesQuery) holds any active generation
-- with zero facts as "emission still in flight" until a
-- code_entities_uid/canonical_nodes_committed phase row exists -- but the
-- global scope publishes no git repository facts and never will, so the
-- lane held forever: deployable_unit_correlation deferred every wave and
-- the B-7 drain never reached terminal. The global scope has no code
-- repositories, so its canonical projection is trivially committed; this
-- row records exactly that.
--
-- Acceptance unit mirrors the terraform-state phase convention
-- (scope-as-unit, generation-as-run): the scope itself is the unit.
--
-- ON CONFLICT DO NOTHING: this directory has no applied-migration ledger,
-- so every file replays on every bootstrap and the seed must converge once
-- and no-op after it.
INSERT INTO graph_projection_phase_state (
    scope_id, acceptance_unit_id, source_run_id, generation_id,
    keyspace, phase, committed_at, updated_at
) VALUES (
    'eshu:global', 'eshu:global', 'eshu:global:genesis', 'eshu:global:genesis',
    'code_entities_uid', 'canonical_nodes_committed',
    clock_timestamp(), clock_timestamp()
) ON CONFLICT (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase) DO NOTHING;
