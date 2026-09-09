// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const activeReducerGraphWorkQuery = `
WITH ` + activeFactWorkItemsCTE + `
SELECT EXISTS (
    SELECT 1
    FROM active_fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('pending', 'retrying', 'claimed', 'running')
      AND domain IN (
        'deployment_mapping',
        'workload_materialization',
        'semantic_entity_materialization',
        'sql_relationship_materialization',
        'shell_exec_materialization',
        'inheritance_materialization'
      )
)
`

// ReducerGraphDrain checks whether reducer graph-writing domains are still
// active before standalone shared-projection lanes write to the same backend.
type ReducerGraphDrain struct {
	queryer Queryer
}

// uncommittedCanonicalCodeScopesQuery reports whether any code scope's active
// generation is still missing its canonical-nodes phase. The projector
// publishes that phase per (scope, repository, source run, generation) from
// the generation's repository facts (projector/runtime_phase.go), so a code
// scope whose active generation holds git repository facts but no matching
// phase row has canonical nodes that are not committed yet — on a first
// ingest, a bulk load, or a rebuild whose refinalize cleared the phases.
//
// The match is per repository, not per scope: one committed repository must
// not release the lane for its uncommitted siblings, because a
// cross-repository edge drained now MATCHes an endpoint that does not exist
// yet and is then marked completed — a silent permanent loss. (#6184 owner
// P2: the probe used to release on ANY phase row for the scope.)
// Repository facts without a repo_id carry no MATCHable identity (the
// projector skips their phase for the same reason) and are excluded rather
// than holding the lane forever.
//
// Non-code scopes never block: a cloud-only scope emits no git repository
// facts and never publishes this phase, so the second branch below only
// holds generations that have committed no facts at all yet (emission still
// in flight). Once any fact for the generation lands, a scope holding only
// non-git facts is definitively non-code and releases, while a code scope's
// repository facts stay governed by the branch above.
// Tombstoned repository facts do not count: a scope whose repositories are
// all retracted has nothing left to commit.
//
// Both subqueries are index-served: fact_records_scope_generation_idx covers
// (scope_id, generation_id, fact_kind), and the phase probe rides the
// graph_projection_phase_state primary key's scope_id prefix with
// generation/keyspace/phase as filters. No new index: this runs at most
// twice per code-call poll cycle per partition (active-work plus this
// check), over a scope-count row set with short-circuit on first match,
// not per edge. (#6184 review F3)
const uncommittedCanonicalCodeScopesQuery = `
SELECT EXISTS (
    SELECT 1
    FROM ingestion_scopes AS scope
    WHERE scope.active_generation_id IS NOT NULL
      AND (
        EXISTS (
            SELECT 1
            FROM fact_records AS fact
            WHERE fact.scope_id = scope.scope_id
              AND fact.generation_id = scope.active_generation_id
              AND fact.fact_kind = 'repository'
              AND fact.source_system = 'git'
              AND fact.is_tombstone = FALSE
              AND NULLIF(fact.payload ->> 'repo_id', '') IS NOT NULL
              AND NOT EXISTS (
                  SELECT 1
                  FROM graph_projection_phase_state AS phase
                  WHERE phase.scope_id = scope.scope_id
                    AND phase.generation_id = scope.active_generation_id
                    AND phase.keyspace = $1
                    AND phase.phase = $2
                    AND phase.acceptance_unit_id = fact.payload ->> 'repo_id'
              )
        )
        OR (
            -- No facts at all are committed for the active generation yet:
            -- emission is still in flight, so the absence of repository
            -- facts proves nothing and the lane holds. Once ANY fact for
            -- the generation lands, its emission happened: a code scope's
            -- repository facts are then governed by the branch above,
            -- while a scope holding only non-git facts is a non-code
            -- (cloud) scope whose canonical-nodes phase will never exist.
            -- Holding such a scope wedged the whole lane in every cell
            -- driving cloud cassettes (#6184: nine GCP scopes held
            -- code-call projection back with six code_calls intents
            -- pending at the drain).
            NOT EXISTS (
                SELECT 1
                FROM fact_records AS fact
                WHERE fact.scope_id = scope.scope_id
                  AND fact.generation_id = scope.active_generation_id
            )
            AND NOT EXISTS (
                SELECT 1
                FROM graph_projection_phase_state AS phase
                WHERE phase.scope_id = scope.scope_id
                  AND phase.generation_id = scope.active_generation_id
                  AND phase.keyspace = $1
                  AND phase.phase = $2
            )
        )
      )
)
`

// NewReducerGraphDrain constructs a reducer graph-drain checker.
func NewReducerGraphDrain(queryer Queryer) ReducerGraphDrain {
	return ReducerGraphDrain{queryer: queryer}
}

// HasActiveReducerGraphWork reports whether graph-writing reducer work is
// pending, retrying, claimed, or running.
func (d ReducerGraphDrain) HasActiveReducerGraphWork(ctx context.Context) (bool, error) {
	if d.queryer == nil {
		return false, fmt.Errorf("reducer graph drain queryer is required")
	}

	rows, err := d.queryer.QueryContext(ctx, activeReducerGraphWorkQuery)
	if err != nil {
		return false, fmt.Errorf("check active reducer graph work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, rows.Err()
	}

	var active bool
	if err := rows.Scan(&active); err != nil {
		return false, fmt.Errorf("scan active reducer graph work: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check active reducer graph work: %w", err)
	}

	return active, nil
}

// HasUncommittedCanonicalCodeScopes reports whether any code scope's active
// generation still lacks its canonical-nodes phase (#6184). Code-call
// projection drains cross-repository edges with a MATCH-only write: an edge
// drained before the callee repository's canonical nodes commit matches
// nothing, writes nothing, raises nothing, and is then marked completed — a
// silent permanent loss (115 of 116 CALLS on the DR fixture corpus). The
// per-intent readiness gate only covers the caller's acceptance unit, so the
// runner holds the whole lane until every code scope's canonical nodes are
// committed. A wedged scope stalls code calls visibly (pending intents +
// BlockedReadiness) instead of losing edges silently; that is the intended
// trade — the same one the active-work check above already makes.
func (d ReducerGraphDrain) HasUncommittedCanonicalCodeScopes(ctx context.Context) (bool, error) {
	if d.queryer == nil {
		return false, fmt.Errorf("reducer graph drain queryer is required")
	}

	rows, err := d.queryer.QueryContext(
		ctx,
		uncommittedCanonicalCodeScopesQuery,
		string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
		string(reducer.GraphProjectionPhaseCanonicalNodesCommitted),
	)
	if err != nil {
		return false, fmt.Errorf("check uncommitted canonical code scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, rows.Err()
	}

	var uncommitted bool
	if err := rows.Scan(&uncommitted); err != nil {
		return false, fmt.Errorf("scan uncommitted canonical code scopes: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check uncommitted canonical code scopes: %w", err)
	}

	return uncommitted, nil
}
