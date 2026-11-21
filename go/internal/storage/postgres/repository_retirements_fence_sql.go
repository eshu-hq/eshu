// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Phase-1 fence statements (#7766). Bindings are the amended tuned form:
// the retire unit is the whole scope, every generation, so 7d binds
// scope_id ANY (not per-generation pairs) and the step-6 recheck binds
// scope_id ANY over both stages. 7c binds the step-4 non-superseded pairs,
// skipping projector rows of generations the #7130 claim branch already
// superseded. Every statement resolving repositories to scopes uses
// retirementScopesOfReposSQL (#10054), so a repository's derived scope is fenced
// with it, and every per-repository count groups by retirementScopeRepoIDSQL.
const (
	fenceResolveScopesQuery = `SELECT s.scope_id, s.partition_key FROM ingestion_scopes AS s
		WHERE ` + retirementScopesOfReposSQL + ` ORDER BY s.scope_id`
	fencePrecheckGenerationsQuery = `SELECT ` + retirementScopeRepoIDSQL + `, count(g.generation_id)
		FROM ingestion_scopes AS s
		LEFT JOIN scope_generations AS g ON g.scope_id = s.scope_id
			AND g.status IN ('pending', 'active', 'failed')
		WHERE ` + retirementScopesOfReposSQL + ` GROUP BY 1`
	fencePrecheckReducerRowsQuery = `SELECT ` + retirementScopeRepoIDSQL + `, count(w.work_item_id)
		FROM ingestion_scopes AS s
		LEFT JOIN fact_work_items AS w ON w.scope_id = s.scope_id
			AND w.stage = 'reducer'
			AND (w.status IN ('pending', 'retrying', 'failed', 'dead_letter')
				OR (w.status IN ('claimed', 'running') AND w.claim_until <= clock_timestamp()))
		WHERE ` + retirementScopesOfReposSQL + ` GROUP BY 1`
	fenceLiveProjectorLeasesQuery = `SELECT work_item_id FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage = 'projector'
			AND status IN ('claimed', 'running') AND claim_until > clock_timestamp() LIMIT 1`
	fenceLiveReducerLeasesQuery = `SELECT work_item_id FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage = 'reducer'
			AND status IN ('claimed', 'running') AND claim_until > clock_timestamp() LIMIT 1`
	fenceLockScopesQuery = `SELECT s.scope_id FROM ingestion_scopes AS s
		WHERE ` + retirementScopesOfReposSQL + ` ORDER BY s.scope_id FOR NO KEY UPDATE`
	fenceGenerationPairsQuery = `SELECT scope_id, generation_id FROM scope_generations
		WHERE scope_id = ANY($1) AND status IN ('pending', 'active', 'failed')`
	fenceReducerRowCountQuery = `SELECT count(*) FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage = 'reducer'
			AND (status IN ('pending', 'retrying', 'failed', 'dead_letter')
				OR (status IN ('claimed', 'running') AND claim_until <= clock_timestamp()))`
	// fenceUnderLockGenerationsQuery recounts the step-4 pair set per
	// repository over the locked scopes, so an under-lock
	// request_too_large refusal carries the same PerRepo breakdown the
	// precheck refusal promises. Same predicate as the pairs query, plus
	// the scope-to-repository join.
	fenceUnderLockGenerationsQuery = `SELECT ` + retirementScopeRepoIDSQL + `, count(g.generation_id)
		FROM ingestion_scopes AS s
		LEFT JOIN scope_generations AS g ON g.scope_id = s.scope_id
			AND g.status IN ('pending', 'active', 'failed')
		WHERE s.scope_id = ANY($1) GROUP BY 1`
	// fenceUnderLockReducerRowsQuery recounts the 7d-predicate reducer
	// rows per repository over the locked scopes. Same predicate as the
	// single-count recheck, grouped.
	fenceUnderLockReducerRowsQuery = `SELECT ` + retirementScopeRepoIDSQL + `, count(w.work_item_id)
		FROM ingestion_scopes AS s
		LEFT JOIN fact_work_items AS w ON w.scope_id = s.scope_id
			AND w.stage = 'reducer'
			AND (w.status IN ('pending', 'retrying', 'failed', 'dead_letter')
				OR (w.status IN ('claimed', 'running') AND w.claim_until <= clock_timestamp()))
		WHERE s.scope_id = ANY($1) GROUP BY 1`
	fenceLockTableQuery         = `LOCK TABLE fact_work_items IN EXCLUSIVE MODE`
	fenceRecheckLiveLeasesQuery = `SELECT work_item_id FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage IN ('projector', 'reducer')
			AND status IN ('claimed', 'running') AND claim_until > clock_timestamp() LIMIT 1`
	fenceSupersedeGenerationsQuery = `UPDATE scope_generations SET status = 'superseded', superseded_at = clock_timestamp()
		WHERE scope_id = ANY($1) AND status IN ('pending', 'active', 'failed')`
	fenceNullActivePointerQuery = `UPDATE ingestion_scopes AS s SET active_generation_id = NULL
		WHERE ` + retirementScopesOfReposSQL
	// Both marks fold the row's prior failure into failure_details through
	// priorFailureWorkSQL (#7320): the 7d predicate supersedes failed and
	// dead_letter rows, and their evidence must survive under prior_failure
	// next to the retirement failure class.
	fenceMarkProjectorRowsQuery = `UPDATE fact_work_items AS work
		SET status = 'superseded', failure_class = '` + retirementFencedFailureClass + `',
			failure_details = jsonb_build_object('retirement_fence', true) || ` + priorFailureWorkSQL + `,
			updated_at = clock_timestamp()
		FROM unnest($1::text[], $2::text[]) AS g(scope_id, generation_id)
		WHERE work.scope_id = g.scope_id AND work.generation_id = g.generation_id
			AND work.stage = 'projector'
			AND (work.status IN ('pending', 'retrying')
				OR (work.status IN ('claimed', 'running') AND work.claim_until <= clock_timestamp()))`
	// fenceMarkReducerRowsQuery marks the 7d-predicate reducer rows
	// superseded and clears their lease-mirror rows in the same statement
	// (#11081: a mark that left the mirror behind orphaned the conflict
	// key). It returns the marked main-table match count (queryTerminalMatchCount
	// reads it), preserving the reducerRows semantics: success depends only
	// on the fence matching, never on mirror presence.
	fenceMarkReducerRowsQuery = `WITH marked AS (
		UPDATE fact_work_items AS work
		SET status = 'superseded', lease_owner = NULL, claim_until = NULL,
			failure_class = '` + retirementFencedFailureClass + `',
			failure_details = jsonb_build_object('retirement_fence', true) || ` + priorFailureWorkSQL + `,
			updated_at = clock_timestamp()
		WHERE work.scope_id = ANY($1) AND work.stage = 'reducer'
			AND (work.status IN ('pending', 'retrying', 'failed', 'dead_letter')
				OR (work.status IN ('claimed', 'running') AND work.claim_until <= clock_timestamp()))
		RETURNING work.work_item_id
	),
	cleared_leases AS (
		DELETE FROM fact_work_item_leases AS lease
		USING marked
		WHERE lease.work_item_id = marked.work_item_id
	)
	SELECT (SELECT count(*) FROM marked)`
	fenceDeleteReindexRequestsQuery = `DELETE FROM repository_reindex_requests WHERE scope_id = ANY($1)`
	fenceAssertLiveGenerationsQuery = `SELECT count(*) FROM scope_generations
		WHERE scope_id = ANY($1) AND status IN ('pending', 'active', 'failed')`
)
