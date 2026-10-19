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
	// 55P03, the same signal the fence restarts on.
	purgeLockScopesQuery = `SELECT scope_id FROM ingestion_scopes
		WHERE partition_key = $1 ORDER BY scope_id FOR NO KEY UPDATE NOWAIT`

	// purgeBlockingGenerationsQuery is the terminal gate readback: every
	// generation still in a live status, grouped for the WARN log. Failed
	// and superseded generations pass: their rows cascade with the scope
	// delete, and the fence already marked their work unclaimable.
	purgeBlockingGenerationsQuery = `SELECT status, count(*) FROM scope_generations
		WHERE scope_id = ANY($1) AND status IN ('pending', 'active')
		GROUP BY status ORDER BY status`

	// purgeGrantCountsQuery pre-counts the four grant tables the scope
	// delete cascades, so the tombstone ledger reports what the cascade
	// dropped. A cascade reports no counts of its own.
	purgeGrantCountsQuery = `SELECT
		(SELECT count(*) FROM tenant_scope_grants WHERE scope_id = ANY($1)),
		(SELECT count(*) FROM tenant_repository_grants WHERE scope_id = ANY($1)),
		(SELECT count(*) FROM identity_role_scope_targets WHERE scope_id = ANY($1)),
		(SELECT count(*) FROM identity_role_repository_targets WHERE scope_id = ANY($1))`

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
