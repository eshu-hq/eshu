// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// SharedIntentStore answers the first-projection probe the shared projection
// runner uses to skip a scope's whole-scope edge retract when there is nothing
// to retract (#3624).
var _ reducer.FirstProjectionLookup = (*SharedIntentStore)(nil)

// scopeHasPriorGenerationSQL reports whether the scope has ANY generation other
// than the current one, in any status. For the shared-projection domains edges
// are written on acceptance (before a generation activates), so a generation
// that was accepted and then superseded while still pending can have written
// edges without ever setting activated_at. Keying the first-projection skip on
// activation would miss those edges; keying it on "no other generation exists at
// all" is correct — the only scope with zero prior edges of any generation is
// one whose sole generation is the current one (a true first projection). On a
// cold first ingest a scope has exactly one generation, so the skip still fires.
// The scope_generations_scope_idx (scope_id, ...) index serves the scope_id
// prefix; generation_id is applied as a cheap post-scan filter bounded by the
// (small) number of generations per scope.
const scopeHasPriorGenerationSQL = `
SELECT EXISTS (
    SELECT 1
    FROM scope_generations
    WHERE scope_id = $1
      AND generation_id <> $2
)`

// ScopeHasPriorGeneration reports whether the scope has any generation other
// than currentGenerationID. When it returns false the scope's only generation
// is the current one — a true first projection — so its whole-scope edge retract
// is a guaranteed no-op and the shared projection runner skips it (#3624).
// Returning true (or an error) leaves the retract running, so every re-ingest,
// and any scope that has ever had another generation project (activated or not),
// still retracts.
func (s *SharedIntentStore) ScopeHasPriorGeneration(
	ctx context.Context,
	scopeID string,
	currentGenerationID string,
) (bool, error) {
	sqlRows, err := s.database.QueryContext(
		ctx,
		scopeHasPriorGenerationSQL,
		scopeID,
		currentGenerationID,
	)
	if err != nil {
		return false, fmt.Errorf("query prior generation for scope %s: %w", scopeID, err)
	}
	defer func() { _ = sqlRows.Close() }()

	if !sqlRows.Next() {
		// SELECT EXISTS always returns exactly one row; no row means an
		// iteration/query error. Returning false here would mislabel the scope a
		// first projection and skip a needed retract, so surface the error and
		// leave the retract running.
		if err := sqlRows.Err(); err != nil {
			return false, fmt.Errorf("iterate prior generation for scope %s: %w", scopeID, err)
		}
		return false, fmt.Errorf("prior generation probe for scope %s returned no row", scopeID)
	}
	var exists bool
	if err := sqlRows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan prior generation for scope %s: %w", scopeID, err)
	}
	return exists, sqlRows.Err()
}

// supersededGenerationIDsSQL returns which of the batch's generation ids are
// superseded scope generations whose prerequisite-phase producers can no longer
// publish. generation_id is scope_generations' primary key, so the ANY() probe
// is a bounded primary-key lookup over at most the distinct generations of one
// selection window, one round trip per pass.
//
// The worker passes only the generation ids of readiness-BLOCKED rows: ready
// rows on a superseded generation still project, because a delta successor
// would never re-emit their edge (#7121, #7130).
//
// # In-flight producer guard
//
// A blocked row is drained only when its generation is superseded AND has no
// in-flight producer, because a producer that is already running when the
// successor activates can still publish the phase row, after which the row is
// ready and must project (a delta successor never re-emits it). The invariant
// that makes "superseded and no in-flight producer" mean "the phase never
// publishes":
//
//   - Reducer stage (workload_materialization for runs_in/handles_route and
//     semantic_entity_materialization for documentation_edges): both reducer
//     claim statements, claimReducerWorkQuery and claimReducerWorkBatchQuery,
//     embed supersedeInactiveReducerGenerationsCTE and exclude the rows it
//     supersedes from the candidate set in the same statement, so an unleased
//     (pending/retrying) reducer row of an older generation is superseded
//     instead of claimed. Only claimed/running rows survive that sweep, and the
//     candidate set re-claims them when their lease expires, so claimed/running
//     counts as in flight whatever claim_until says.
//   - Projector stage (canonical_nodes for the code-call, inheritance, sql,
//     shell and rationale edge domains): the projector claim supersedes only
//     rows of pending/failed generations that have a newer projector sibling,
//     so a pending or retrying projector row of an already-superseded
//     generation is still claimable and counts as in flight too.
//
// fact_work_items_scope_generation_idx (scope_id, generation_id, status,
// updated_at) makes the NOT EXISTS an index probe. A deferred generation is
// re-checked on the next selection pass, so it drains once the producer
// finishes without publishing (its lease expired and the sweep superseded it)
// and stays ready-and-projected when it did publish. The one path that can
// still lose an edge is a superseded generation re-activated by ack (#7130).
//
// It keys on the terminal 'superseded' status, not on "generation_id is not
// ingestion_scopes.active_generation_id": a pending generation's shared
// intents are selectable before it activates, so "not active" would race with
// activation and drop live work. Ids that are not scope generations (for
// example repo_dependency resolver relationship-generation ids) match nothing
// and stay selectable.
const supersededGenerationIDsSQL = `
SELECT g.generation_id
FROM scope_generations AS g
WHERE g.generation_id = ANY($1::text[])
  AND g.status = 'superseded'
  AND NOT EXISTS (
    SELECT 1
    FROM fact_work_items AS w
    WHERE w.scope_id = g.scope_id
      AND w.generation_id = g.generation_id
      AND (
        (w.stage = 'reducer' AND w.status IN ('claimed', 'running'))
        OR (w.stage = 'projector' AND w.status IN ('pending', 'retrying', 'claimed', 'running'))
      )
  )
`

// SupersededGenerationIDs returns the subset of generationIDs whose scope
// generation is superseded and has no in-flight producer work item (see
// supersededGenerationIDsSQL). The shared projection worker uses it to drain
// readiness-blocked intents whose generation will never publish the
// prerequisite phase row (#7121); a superseded generation with a claimed or
// running producer is left out and re-checked on a later pass. An empty input
// performs no query; a query error is returned so the caller fails the
// selection instead of guessing.
func (s *SharedIntentStore) SupersededGenerationIDs(
	ctx context.Context,
	generationIDs []string,
) (map[string]struct{}, error) {
	if len(generationIDs) == 0 {
		return nil, nil
	}

	sqlRows, err := s.database.QueryContext(ctx, supersededGenerationIDsSQL, generationIDs)
	if err != nil {
		return nil, fmt.Errorf("query superseded generations: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	superseded := make(map[string]struct{})
	for sqlRows.Next() {
		var generationID string
		if err := sqlRows.Scan(&generationID); err != nil {
			return nil, fmt.Errorf("scan superseded generation: %w", err)
		}
		superseded[generationID] = struct{}{}
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate superseded generations: %w", err)
	}
	return superseded, nil
}
