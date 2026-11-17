// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Narrowed purge statements (#7766 increment 3). The purge deletes ONLY
// ingestion_scopes rows plus the scope- and repo-keyed leftover rows that
// carry no foreign key (the table-census 2e core set): generations and every
// generation-FK table go through the scope-row ON DELETE CASCADE, and every
// scope-FK table (grants, claim fences, derived state) cascades from the
// same delete. Nothing here touches fact tables directly and nothing touches
// the graph, so contract A holds by construction.
const (
	// purgeClaimTombstoneQuery locks the repository's open tombstone row.
	// The FOR UPDATE serializes concurrent purgers on the same repository:
	// the loser re-reads the row and no-ops on complete.
	purgeClaimTombstoneQuery = `SELECT retirement_id, repo_id, scope_id, state, phase
		FROM repository_retirements WHERE repo_id = $1 AND readmitted_at IS NULL FOR UPDATE`

	// purgeLockScopesQuery locks the repository's scope rows without
	// waiting: a concurrent retention pass holding a scope lock reports
	// scope_lock_busy instead of queueing behind the purge. NOWAIT raises
	// 55P03, the same signal the fence restarts on. The set includes the
	// repository's derived scope (#10054), which a projector Ack may hold just
	// as retention holds the others.
	purgeLockScopesQuery = `SELECT s.scope_id FROM ingestion_scopes AS s
		WHERE ` + retirementScopesOfRepoSQL + ` ORDER BY s.scope_id FOR NO KEY UPDATE NOWAIT`

	// purgeBlockingGenerationsQuery is the terminal gate readback: every
	// generation still in a live status, grouped for the WARN log. Failed
	// and superseded generations pass: their rows cascade with the scope
	// delete, and the fence already marked their work unclaimable.
	purgeBlockingGenerationsQuery = `SELECT status, count(*) FROM scope_generations
		WHERE scope_id = ANY($1) AND status IN ('pending', 'active')
		GROUP BY status ORDER BY status`

	// purgeGrantCountsQuery pre-counts the four grant tables the scope
	// delete cascades, plus the service evidence snapshots the leftover
	// service_materialization_generations delete cascades (#10820), so the
	// tombstone ledger reports what a cascade dropped. A cascade reports no
	// counts of its own.
	purgeGrantCountsQuery = `SELECT
		(SELECT count(*) FROM tenant_scope_grants WHERE scope_id = ANY($1)),
		(SELECT count(*) FROM tenant_repository_grants WHERE scope_id = ANY($1)),
		(SELECT count(*) FROM identity_role_scope_targets WHERE scope_id = ANY($1)),
		(SELECT count(*) FROM identity_role_repository_targets WHERE scope_id = ANY($1)),
		(SELECT count(*) FROM service_evidence_snapshots WHERE generation_id IN
			(SELECT generation_id FROM service_materialization_generations WHERE scope_id = ANY($1)))`

	// purgeDeleteReindexRequestsQuery drops the scope-keyed reindex rows.
	// The fence already deleted them; the purge re-issues the delete so a
	// row re-added between fence and purge cannot survive the retirement.
	purgeDeleteReindexRequestsQuery       = `DELETE FROM repository_reindex_requests WHERE scope_id = ANY($1)`
	purgeDeleteSelectionObservationsQuery = `DELETE FROM repository_selection_observations WHERE scope_id = ANY($1)`

	// purgeDeleteContentQueries drop the repo-keyed content rows, which
	// carry no generation or scope foreign key by design and survive the
	// scope cascade otherwise.
	purgeDeleteContentFilesQuery      = `DELETE FROM content_files WHERE repo_id = $1`
	purgeDeleteContentEntitiesQuery   = `DELETE FROM content_entities WHERE repo_id = $1`
	purgeDeleteContentReferencesQuery = `DELETE FROM content_file_references WHERE repo_id = $1`
	purgeDeleteRepositoryRefsQuery    = `DELETE FROM repository_refs WHERE repo_id = $1`

	// purgeDeleteIntentQueries drop the projection-intent rows, which
	// carry no foreign key by design (plain TEXT keys, see migration 008
	// and 098) and survive the scope cascade otherwise. The predicate
	// matches the preview's SharedIntents count exactly, so the dry run
	// and the ledger agree.
	purgeDeleteSharedIntentsQuery = `DELETE FROM shared_projection_intents
		WHERE repository_id = $1 OR scope_id = ANY($2)`
	purgeDeleteUnroutableIntentsQuery = `DELETE FROM shared_projection_unroutable_intents
		WHERE repository_id = $1 OR scope_id = ANY($2)`
	// purgeDeleteDeadLettersQuery drops the collector dead letters of the
	// purge scopes. Dead letters key on generation with no foreign key,
	// so the scope predicate reaches exactly the generations the scope
	// delete cascades.
	purgeDeleteDeadLettersQuery = `DELETE FROM collector_generation_dead_letters
		WHERE scope_id = ANY($1)`

	// purgeDeleteWriteAdmissionQuery drops the #7142 impact write-admission
	// watermark rows of the purge scopes (migration 154, no FK; the primary
	// key leads with scope_id). It runs in the leftover sweep, BEFORE the
	// scope delete, which is the opposite of the pass-state row: the impact
	// writer upserts the admission row before its fact insert takes the
	// KEY SHARE on scope_generations (writer_retract.go), so the writer's
	// order is (admission row, generation row). The purge takes the same
	// order: admission row here, generation rows in the cascade. Deleting
	// after the cascade would hold the generation lock while waiting on an
	// admission row an in-flight writer holds, and that writer waits on the
	// generation: a cycle only the deadlock detector could break.
	purgeDeleteWriteAdmissionQuery = `DELETE FROM supply_chain_impact_write_admission
		WHERE scope_id = ANY($1)`

	// The #10820 leftover statements. Each table is plain TEXT-keyed with no
	// foreign key, so the scope cascade never reaches it. The census rows
	// (reducer/retirement/census.go) and docs/internal/evidence/10820-census-2e-purge.md
	// carry the per-table writer, key and lock-order evidence; the positions
	// that matter are:
	//
	//   - before the content deletes: the infra and config-artifact advisory
	//     locks (purgeLockInfraRepositoryQuery's helpers), in retention's order;
	//   - after the content_entities delete: infra_resource_entity_dirty_repos,
	//     because the content delete re-marks the repository;
	//   - after the ingestion_scopes delete: relationship_generations (the
	//     writer locks its fact_work_items row first, which the cascade
	//     locks) and the consumption dirty-scope mark (defensive: the
	//     cascade's fact_records trigger would re-mark it, though the sidecar
	//     prune earlier in the transaction currently suppresses that).
	purgeDeleteReadinessWaitsQuery = `DELETE FROM reducer_readiness_waits WHERE scope_id = ANY($1)`
	// purgeDeleteProjectionDecisionEvidenceQuery runs before its parent: the
	// evidence rows carry no repository column, only the decision id.
	purgeDeleteProjectionDecisionEvidenceQuery = `DELETE FROM projection_decision_evidence
		WHERE decision_id IN (SELECT decision_id FROM projection_decisions WHERE repository_id = $1)`
	purgeDeleteProjectionDecisionsQuery     = `DELETE FROM projection_decisions WHERE repository_id = $1`
	purgeDeleteCrossplaneRedriveStateQuery  = `DELETE FROM crossplane_satisfied_by_redrive_state WHERE xrd_scope_id = ANY($1)`
	purgeDeleteCrossplaneRedriveLedgerQuery = `DELETE FROM crossplane_satisfied_by_redrive_target_ledger WHERE target_scope_id = ANY($1)`
	purgeDeleteServiceMaterializationQuery  = `DELETE FROM service_materialization_generations WHERE scope_id = ANY($1)`
	purgeDeleteFingerprintsQuery            = `DELETE FROM code_function_fingerprint WHERE repo_id = $1`
	purgeDeleteFingerprintBandsQuery        = `DELETE FROM code_fingerprint_band WHERE repo_id = $1`
	purgeDeleteFunctionSummariesQuery       = `DELETE FROM function_summaries WHERE repo = $1`
	purgeDeleteFunctionSourcesQuery         = `DELETE FROM function_sources WHERE repo = $1`
	purgeDeleteFunctionGraphIDsQuery        = `DELETE FROM function_graph_ids WHERE repo = $1`
	purgeDeleteContentRefDeltasQuery        = `DELETE FROM content_file_ref_deltas WHERE repo_id = $1`
	purgeDeleteContentRefDeltaBasesQuery    = `DELETE FROM content_file_ref_delta_bases WHERE repo_id = $1`
	purgeDeleteConfigArtifactPathsQuery     = `DELETE FROM config_artifact_paths WHERE repo_id = $1`
	purgeDeleteInfraEntitiesQuery           = `DELETE FROM infra_resource_entities WHERE repo_id = $1`
	purgeDeleteInfraDirtyReposQuery         = `DELETE FROM infra_resource_entity_dirty_repos WHERE repo_id = $1`
	purgeDeleteConsumptionDirtyScopesQuery  = `DELETE FROM package_manifest_consumption_key_dirty_scopes WHERE scope_id = ANY($1)`
	purgeDeleteRelationshipGenerationsQuery = `DELETE FROM relationship_generations WHERE scope = ANY($1)`

	// purgeRelationshipGenerationIDsSQL selects the generation ids of the purge
	// scopes from both maps: scope_generations (every generation the scope
	// ever had, still present before the cascade) and relationship_generations
	// (the generations cross-repo resolution activated). The relationship
	// tables carry no scope column, so the generation id is their only link.
	purgeRelationshipGenerationIDsSQL = `(SELECT generation_id FROM scope_generations WHERE scope_id = ANY($1)
			UNION SELECT generation_id FROM relationship_generations WHERE scope = ANY($1))`
	purgeDeleteRelationshipEvidenceQuery   = `DELETE FROM relationship_evidence_facts WHERE generation_id IN ` + purgeRelationshipGenerationIDsSQL
	purgeDeleteRelationshipCandidatesQuery = `DELETE FROM relationship_candidates WHERE generation_id IN ` + purgeRelationshipGenerationIDsSQL

	// purgeScopeGenerationIDsQuery lists every generation of the purge
	// scopes so the key sidecar prune covers exactly the facts the scope
	// delete cascades. Facts are generation-scoped and every key writer
	// copies the fact's scope and generation (facts/keys.DeleteGenerations
	// premise), so pruning per (scope, generation) removes exactly the
	// sidecar rows the per-fact cascade would orphan.
	purgeScopeGenerationIDsQuery = `SELECT scope_id, generation_id FROM scope_generations
		WHERE scope_id = ANY($1) ORDER BY scope_id, generation_id`

	// purgeDeleteScopesQuery deletes the scope rows. The scope_id
	// generation foreign key cascades scope_generations, and through it
	// every generation-FK table; every scope-FK table cascades directly.
	// There is no self-FK on ingestion_scopes, so delete order among the
	// scopes is unobservable and one statement deletes them all. The key
	// sidecar rows do NOT cascade: purgeOne prunes them first with
	// factkeys.DeleteGenerations (paired classification, see the delete
	// guard), or orphaned sidecar rows would survive the retirement.
	purgeDeleteScopesQuery = `DELETE FROM ingestion_scopes WHERE scope_id = ANY($1)`

	// purgeDeleteScopePassStateQuery drops the #10389 per-scope pass-state
	// rows (migration 284, no FK). It runs after the scope delete: the
	// impact writer holds the fact insert's KEY SHARE on scope_generations
	// before it upserts this row last, so deleting here keeps the purge's
	// lock order equal to the writer's (scope_generations, then pass state)
	// and no cycle can form.
	//
	// #nosec G101 -- SQL DML whose const name contains "Pass"; the value is a fully-parameterized query, not a credential literal
	purgeDeleteScopePassStateQuery = `DELETE FROM supply_chain_impact_scope_pass_state WHERE scope_id = ANY($1)`

	// purgeCompleteTombstoneQuery writes the tombstone: the terminal
	// complete/done state, the retirement timestamp, and the deleted-row
	// ledger merged over any prior counts. The state predicate makes a
	// concurrent completion attempt affect zero rows instead of
	// double-counting.
	purgeCompleteTombstoneQuery = `UPDATE repository_retirements
		SET state = 'complete', phase = 'done', retired_at = clock_timestamp(),
			rows_deleted = rows_deleted || $2::jsonb, updated_at = clock_timestamp()
		WHERE retirement_id = $1 AND state = 'pending'`
)
