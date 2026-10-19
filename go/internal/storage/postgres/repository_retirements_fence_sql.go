// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Phase-1 fence statements (#7766). Bindings are the amended tuned form:
// the retire unit is the whole scope, every generation, so 7d binds
// scope_id ANY (not per-generation pairs) and the step-6 recheck binds
// scope_id ANY over both stages. 7c binds the step-4 non-superseded pairs,
// skipping projector rows of generations the #7130 claim branch already
// superseded.
const (
	fenceResolveScopesQuery = `SELECT scope_id, partition_key FROM ingestion_scopes
		WHERE partition_key = ANY($1) ORDER BY scope_id`
	fencePrecheckGenerationsQuery = `SELECT s.partition_key, count(g.generation_id)
		FROM ingestion_scopes AS s
		LEFT JOIN scope_generations AS g ON g.scope_id = s.scope_id
			AND g.status IN ('pending', 'active', 'failed')
		WHERE s.partition_key = ANY($1) GROUP BY s.partition_key`
	fencePrecheckReducerRowsQuery = `SELECT s.partition_key, count(w.work_item_id)
		FROM ingestion_scopes AS s
		LEFT JOIN fact_work_items AS w ON w.scope_id = s.scope_id
			AND w.stage = 'reducer'
			AND (w.status IN ('pending', 'retrying', 'failed', 'dead_letter')
				OR (w.status IN ('claimed', 'running') AND w.claim_until <= clock_timestamp()))
		WHERE s.partition_key = ANY($1) GROUP BY s.partition_key`
	fenceLiveProjectorLeasesQuery = `SELECT work_item_id FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage = 'projector'
			AND status IN ('claimed', 'running') AND claim_until > clock_timestamp() LIMIT 1`
	fenceLiveReducerLeasesQuery = `SELECT work_item_id FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage = 'reducer'
			AND status IN ('claimed', 'running') AND claim_until > clock_timestamp() LIMIT 1`
	fenceLockScopesQuery = `SELECT scope_id FROM ingestion_scopes
		WHERE partition_key = ANY($1) ORDER BY scope_id FOR NO KEY UPDATE`
	fenceGenerationPairsQuery = `SELECT scope_id, generation_id FROM scope_generations
		WHERE scope_id = ANY($1) AND status IN ('pending', 'active', 'failed')`
	fenceReducerRowCountQuery = `SELECT count(*) FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage = 'reducer'
			AND (status IN ('pending', 'retrying', 'failed', 'dead_letter')
				OR (status IN ('claimed', 'running') AND claim_until <= clock_timestamp()))`
	fenceLockTableQuery         = `LOCK TABLE fact_work_items IN EXCLUSIVE MODE`
	fenceRecheckLiveLeasesQuery = `SELECT work_item_id FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage IN ('projector', 'reducer')
			AND status IN ('claimed', 'running') AND claim_until > clock_timestamp() LIMIT 1`
	fenceSupersedeGenerationsQuery = `UPDATE scope_generations SET status = 'superseded', superseded_at = clock_timestamp()
		WHERE scope_id = ANY($1) AND status IN ('pending', 'active', 'failed')`
	fenceNullActivePointerQuery = `UPDATE ingestion_scopes SET active_generation_id = NULL
		WHERE partition_key = ANY($1)`
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
	fenceMarkReducerRowsQuery = `UPDATE fact_work_items AS work
		SET status = 'superseded', failure_class = '` + retirementFencedFailureClass + `',
			failure_details = jsonb_build_object('retirement_fence', true) || ` + priorFailureWorkSQL + `,
			updated_at = clock_timestamp()
		WHERE work.scope_id = ANY($1) AND work.stage = 'reducer'
			AND (work.status IN ('pending', 'retrying', 'failed', 'dead_letter')
				OR (work.status IN ('claimed', 'running') AND work.claim_until <= clock_timestamp()))`
	fenceDeleteReindexRequestsQuery = `DELETE FROM repository_reindex_requests WHERE scope_id = ANY($1)`
	fenceAssertLiveGenerationsQuery = `SELECT count(*) FROM scope_generations
		WHERE scope_id = ANY($1) AND status IN ('pending', 'active', 'failed')`
)
