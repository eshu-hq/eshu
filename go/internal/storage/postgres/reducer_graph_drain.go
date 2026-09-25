// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const activeReducerGraphWorkQuery = `
WITH ` + activeFactWorkItemsPerRowCTE + `
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
	queryer db.Queryer
}

// uncommittedCanonicalCodeScopePredicate is the WHERE clause, over
// ingestion_scopes AS scope, that selects code scopes whose active generation
// is still missing its canonical-nodes phase. The projector publishes that
// phase per (scope, repository, source run, generation) from the generation's
// repository facts (projector/runtime/phase.go), so a code scope whose active
// generation holds git repository facts but no matching phase row has
// canonical nodes that are not committed yet — on a first ingest, a bulk load,
// or a rebuild whose refinalize cleared the phases.
//
// Only code-bearing scopes are eligible (#7133). $3 is the set of collector
// kinds whose contract requires the code_entities_uid canonical-nodes phase
// (workflow.CollectorKindsRequiringPhase, today just git), and $4 excludes
// the git collector's non-default-branch scope kind (repository_ref), which
// the projector gates before any canonical write. A scope outside that set can
// never publish the phase, so letting it hold the lane wedges the lane
// forever: on ops-qa a zero-fact AWS region scope and the synthetic
// eshu:global scope (migration 115, whose migration-116 escape-hatch phase row
// had gone missing) held code_calls and repo_dependency shut for eight days
// while every git scope was committed. Neither can be a CALLS or DEPENDS_ON
// endpoint, so excluding them keeps the #6184 guarantee: every scope that can
// own a MATCHed Repository or code node still holds the lane until committed.
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
// The second branch holds a code scope whose active generation has committed
// no facts at all yet (emission still in flight). Once any fact for the
// generation lands, its repository facts are governed by the first branch,
// and a code scope holding only non-git facts releases.
// Tombstoned repository facts do not count: a scope whose repositories are
// all retracted has nothing left to commit.
//
// Both subqueries are index-served: fact_records_scope_generation_idx covers
// (scope_id, generation_id, fact_kind), and the phase probe rides the
// graph_projection_phase_state primary key's scope_id prefix with
// generation/keyspace/phase as filters. The collector/scope-kind filter is a
// row filter on ingestion_scopes that runs before either subquery, so non-code
// scopes now cost no subquery at all. No new index: this runs at most twice
// per code-call poll cycle per partition over a scope-count row set with
// short-circuit on first match, not per edge. (#6184 review F3, #7133 plan
// evidence in docs/internal/evidence/7133-code-quiescence-scope.md)
const uncommittedCanonicalCodeScopePredicate = `
    scope.active_generation_id IS NOT NULL
      AND scope.collector_kind = ANY($3::text[])
      AND scope.scope_kind <> $4
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
            -- No facts at all are committed for the code scope's active
            -- generation yet: emission is still in flight, so the absence of
            -- repository facts proves nothing and the lane holds.
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
`

// uncommittedCanonicalCodeScopesQuery is the per-cycle gate probe: an EXISTS
// over uncommittedCanonicalCodeScopePredicate that stops at the first match.
const uncommittedCanonicalCodeScopesQuery = `
SELECT EXISTS (
    SELECT 1
    FROM ingestion_scopes AS scope
    WHERE` + uncommittedCanonicalCodeScopePredicate + `)
`

// uncommittedCanonicalCodeScopeSampleQuery names the scopes holding the gate
// for operators (#7133). It is not on the per-cycle path: runners call it only
// when a blocked episode starts and then at a bounded interval. It returns at
// most $5 scope ids in scope_id order plus the total blocking count, which the
// window aggregate computes before LIMIT applies.
const uncommittedCanonicalCodeScopeSampleQuery = `
SELECT scope.scope_id, count(*) OVER () AS blocking_scopes
FROM ingestion_scopes AS scope
WHERE` + uncommittedCanonicalCodeScopePredicate + `ORDER BY scope.scope_id
LIMIT $5
`

// excludedCanonicalCodeScopeKind is the git collector scope kind the projector
// never canonically projects (projector/runtime/projection.go gates it).
const excludedCanonicalCodeScopeKind = string(scope.KindRepositoryRef)

// canonicalCodeCollectorKinds lists the collector kinds whose contract
// requires the code_entities_uid canonical-nodes phase; derived once from
// the workflow collector contracts rather than kept by hand.
var canonicalCodeCollectorKinds = func() []string {
	kinds := workflow.CollectorKindsRequiringPhase(
		reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		reducer.GraphProjectionPhaseCanonicalNodesCommitted,
	)
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, string(kind))
	}
	return out
}()

// canonicalCodeQuiescenceArgs returns the shared $1..$4 arguments.
func canonicalCodeQuiescenceArgs() []any {
	return []any{
		string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
		string(reducer.GraphProjectionPhaseCanonicalNodesCommitted),
		canonicalCodeCollectorKinds,
		excludedCanonicalCodeScopeKind,
	}
}

// NewReducerGraphDrain constructs a reducer graph-drain checker.
func NewReducerGraphDrain(queryer db.Queryer) ReducerGraphDrain {
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
// committed. Only scopes whose collector is contracted to publish that phase
// count (#7133); see uncommittedCanonicalCodeScopePredicate. A wedged scope stalls code calls visibly (pending intents +
// BlockedReadiness) instead of losing edges silently; that is the intended
// trade — the same one the active-work check above already makes.
func (d ReducerGraphDrain) HasUncommittedCanonicalCodeScopes(ctx context.Context) (bool, error) {
	if d.queryer == nil {
		return false, fmt.Errorf("reducer graph drain queryer is required")
	}

	rows, err := d.queryer.QueryContext(ctx, uncommittedCanonicalCodeScopesQuery, canonicalCodeQuiescenceArgs()...)
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

// MaxCanonicalCodeQuiescenceBlockerSample caps how many blocking scope ids
// DescribeUncommittedCanonicalCodeScopes returns, so operator logs stay
// bounded however many scopes hold the gate.
const MaxCanonicalCodeQuiescenceBlockerSample = 20

// DescribeUncommittedCanonicalCodeScopes returns how many scopes currently
// hold the canonical-code quiescence gate and up to limit of their scope ids
// in scope_id order (#7133). It evaluates the same predicate as
// HasUncommittedCanonicalCodeScopes but without the EXISTS short-circuit, so
// callers use it only on a blocked episode's start and at a bounded interval,
// never per cycle. A limit outside 1..MaxCanonicalCodeQuiescenceBlockerSample
// is clamped to that range.
func (d ReducerGraphDrain) DescribeUncommittedCanonicalCodeScopes(
	ctx context.Context,
	limit int,
) (int, []string, error) {
	if d.queryer == nil {
		return 0, nil, fmt.Errorf("reducer graph drain queryer is required")
	}
	if limit <= 0 || limit > MaxCanonicalCodeQuiescenceBlockerSample {
		limit = MaxCanonicalCodeQuiescenceBlockerSample
	}

	args := append(canonicalCodeQuiescenceArgs(), limit)
	rows, err := d.queryer.QueryContext(ctx, uncommittedCanonicalCodeScopeSampleQuery, args...)
	if err != nil {
		return 0, nil, fmt.Errorf("describe uncommitted canonical code scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	total := 0
	scopeIDs := make([]string, 0, limit)
	for rows.Next() {
		var (
			scopeID string
			count   int64
		)
		if err := rows.Scan(&scopeID, &count); err != nil {
			return 0, nil, fmt.Errorf("scan uncommitted canonical code scope: %w", err)
		}
		scopeIDs = append(scopeIDs, scopeID)
		total = int(count)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("describe uncommitted canonical code scopes: %w", err)
	}
	return total, scopeIDs, nil
}
