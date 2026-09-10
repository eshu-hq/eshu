// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rebuildreset

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// Execer is the narrow write surface this package needs from a transaction.
//
// It is declared here rather than imported from the parent postgres package so
// the dependency runs one way only: postgres imports rebuildreset, never the
// reverse. postgres.Transaction satisfies it through its Executor embed.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// AffectedGenerationsTemplate reads the (scope_id, generation_id) pairs one
// refinalize covers. The caller runs it once, at the top of the refinalize
// transaction, and every later statement binds the rows it returned instead of
// re-deriving them.
//
// That is the whole point of reading it separately. A transaction Postgres
// starts at the default READ COMMITTED isolation gives each statement its own
// snapshot, so an ingester that activates a new generation mid-refinalize can
// make the enqueue and the resets disagree: the enqueue re-projects G1 while a
// later reset deletes G2's dedup state, which both leaves G1 deduplicated and
// damages G2 without replaying it. One read, four bindings, no disagreement --
// and no lock on ingestion_scopes, so an activation racing a rebuild is delayed
// by nothing and simply lands outside this refinalize.
//
// The %s is the scope predicate: empty for the all-scopes disaster-recovery
// path, `AND scope.scope_id = ANY($N)` for an explicit scope list.
const AffectedGenerationsTemplate = `
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.active_generation_id IS NOT NULL
  AND scope.status = 'active'
  %s
ORDER BY scope.scope_id
`

// affectedPairs renders the materialized generation set as a two-array unnest,
// binding $first for the scope ids and $first+1 for the generation ids. The
// reset statements take no other argument and start at $1; the projector
// re-enqueue carries a leading timestamp and starts at $2.
//
// Passing the pairs as parallel arrays rather than re-running the SELECT is what
// makes the four statements agree by construction: they cannot select a
// different set, because none of them selects anything.
func affectedPairs(first int) string {
	return fmt.Sprintf(
		"SELECT * FROM unnest($%d::text[], $%d::text[]) AS affected(scope_id, generation_id)",
		first, first+1,
	)
}

// deleteSucceededReducerWorkTemplate removes the succeeded reducer work items
// whose existence would otherwise make the re-projection's enqueue a no-op.
//
// Scoped to 'succeeded' on purpose. Claimed and running rows hold live leases
// that a rebuild must not yank; dead_letter and failed rows belong to the replay
// endpoint and contributed nothing to the pre-wipe graph, so leaving them alone
// costs the rebuild no truth.
const deleteSucceededReducerWorkTemplate = `
DELETE FROM fact_work_items
WHERE stage = 'reducer'
  AND status = 'succeeded'
  AND (scope_id, generation_id) IN (%s)
`

// reopenSharedIntentsTemplate clears completed_at so the partition workers drain
// these intents again.
//
// Cleared, not deleted: the payload is the drain's input, and a reducer domain
// that does not re-emit an identical intent this time round would otherwise lose
// its edges entirely. The `completed_at IS NOT NULL` guard makes the reported
// count mean "reopened" rather than "matched", so a second refinalize over the
// same generations reports zero instead of re-counting its own work.
const reopenSharedIntentsTemplate = `
UPDATE shared_projection_intents
SET completed_at = NULL
WHERE completed_at IS NOT NULL
  AND (scope_id, generation_id) IN (%s)
`

// clearReadinessPhaseStateTemplate re-arms the readiness gates by deleting the
// phase rows that survived the graph wipe. The re-projection republishes each
// phase as it commits, so the gates hold work until its inputs genuinely exist
// rather than waving it through on an answer about a graph that is gone.
//
// The cross_repo_evidence keyspace is deliberately spared. Those rows attest
// Postgres evidence-fact completeness, which a graph-only rebuild preserves:
// wiping them strands the re-projection's cross-repo resolution in deferral,
// because the ingestion backfill fan-in that publishes them never re-runs on a
// projection-only rebuild (#6184). Graph-write phases describe the wiped graph
// and must re-arm; evidence phases describe the preserved facts and must not.
const clearReadinessPhaseStateTemplate = `
DELETE FROM graph_projection_phase_state
WHERE (scope_id, generation_id) IN (%s)
  AND keyspace <> 'cross_repo_evidence'
`

// retireResolutionGenerationsTemplate supersedes the active relationship
// generations for the refinalized pairs, pairing the phase re-arm above.
// Without it the re-projection's by-repos resolved read keeps serving the
// prior wave's rows as current: relationship_generations is the one truth
// surface the phase wipe does not touch, so a retired-no-longer-active
// generation is the only signal that tells downstream consumers their input
// is being recomputed rather than complete (#6184). The re-projection's
// resolution re-activates each generation (upsert) once it resolves from the
// preserved facts, so retirement is a fence, not a loss.
//
// The NOT EXISTS clause is the atomic half of the in-flight reducer fence
// (Codex #6184 P1): retirement commits only when no reducer work item holds a
// live lease on the refinalized pairs. A resolver that claimed before the
// refinalize but resolves through it would otherwise get its generation
// retired mid-flight, then re-activate it with stale rows while its success
// ack dedupes the re-emitted intent. The polite half — a bounded drain wait —
// lives in the caller's RefinalizeScopeProjections; this guard closes the
// poll-to-commit window atomically, in the same statement as the retirement.
// The live-lease predicate mirrors the claim system: claim_until > now() means
// a worker is (or may still be) executing, while NULL or expired means the row
// is reclaimable and whoever reclaims it resolves post-retirement, which is
// the legitimate re-projection direction. now() is the transaction start, so
// the guard sees the same lease clock as the drain wait in its transaction.
// The subquery re-unnests the caller's $1/$2 arrays, so the guard fences
// exactly the set the outer IN retires, with no new bind arguments.
const retireResolutionGenerationsTemplate = `
UPDATE relationship_generations
SET status = 'superseded'
WHERE status = 'active'
  AND (scope, generation_id) IN (%s)
  AND NOT EXISTS (
    SELECT 1
    FROM fact_work_items AS w
    WHERE w.stage = 'reducer'
      AND w.status IN ('claimed', 'running')
      AND w.claim_until > now()
      AND (w.scope_id, w.generation_id) IN (
        SELECT * FROM unnest($1::text[], $2::text[]) AS fenced(scope_id, generation_id)
      )
  )
`

// scopePredicate renders the scope filter for the one statement that still reads
// ingestion_scopes, plus the arg that fills it.
//
// The all-scopes path drops the clause rather than passing an empty array,
// because `scope_id = ANY('{}')` matches no rows: a rebuild that silently
// selected nothing would report success and leave the graph empty.
func scopePredicate(filter recovery.RefinalizeFilter) (string, []any) {
	if filter.AllScopes {
		return "", nil
	}

	return "AND scope.scope_id = ANY($1)", []any{filter.ScopeIDs}
}

// AffectedGenerationsQuery renders the read that materializes one refinalize's
// generation set, plus the args that fill it. The caller runs it inside the
// refinalize transaction and passes the result to Apply and to its own projector
// re-enqueue.
func AffectedGenerationsQuery(filter recovery.RefinalizeFilter) (string, []any) {
	predicate, args := scopePredicate(filter)
	return fmt.Sprintf(AffectedGenerationsTemplate, predicate), args
}

// Generations is one refinalize's materialized generation set, held as parallel
// arrays because that is the shape every statement binds.
//
// The two slices are index-aligned: ScopeIDs[i] holds GenerationIDs[i]. Nothing
// enforces that in the type, so build it with Append rather than assembling the
// slices separately.
type Generations struct {
	ScopeIDs      []string
	GenerationIDs []string
}

// Append records one (scope_id, generation_id) pair, keeping the two slices
// aligned.
func (g *Generations) Append(scopeID, generationID string) {
	g.ScopeIDs = append(g.ScopeIDs, scopeID)
	g.GenerationIDs = append(g.GenerationIDs, generationID)
}

// Len reports how many generations the refinalize covers.
func (g Generations) Len() int { return len(g.ScopeIDs) }

// Args returns the generation set as statement arguments, in the order
// affectedPairs binds them.
func (g Generations) Args() []any {
	return []any{g.ScopeIDs, g.GenerationIDs}
}

// buildResetQuery renders one reset statement against a materialized generation
// set. The reset statements take no timestamp, so their arrays start at $1.
func buildResetQuery(template string, generations Generations) (string, []any) {
	return fmt.Sprintf(template, affectedPairs(1)), generations.Args()
}

// Counts reports how much dedup state one refinalize cleared. An operator
// running a recovery needs these to distinguish "the rebuild re-queued the
// scopes" from "the rebuild will actually rebuild the whole graph"; before this
// existed, the two were indistinguishable until the graph came back short.
type Counts struct {
	// ReducerWorkDeleted counts succeeded reducer work items removed so the
	// re-projection's enqueue can land them again.
	ReducerWorkDeleted int
	// SharedIntentsReopened counts completed shared projection intents whose
	// completed_at was cleared so the partition workers drain them again.
	SharedIntentsReopened int
	// ReadinessPhasesCleared counts graph projection phase rows deleted so the
	// readiness gates stop answering about a graph that was wiped.
	ReadinessPhasesCleared int
	// GenerationsRetired counts active relationship generations superseded so
	// the re-projection never consumes the prior wave's resolved rows as
	// current truth.
	GenerationsRetired int
}

// Apply clears the four pieces of dedup state that would otherwise make a
// rebuild-from-facts stop at source-local structure. It runs inside the caller's
// transaction so a refinalize either re-enqueues the projector work and reopens
// its downstream state together, or does neither.
//
// generations is the set the caller already materialized with
// AffectedGenerationsQuery, in the same transaction, before it enqueued the
// projector work. Passing it in rather than re-selecting it is the point: under
// READ COMMITTED a re-selection could pick up a generation the enqueue never saw.
//
// All four statements touch terminal state only, so no live lease is taken away
// and no claimed item can double-execute.
func Apply(
	ctx context.Context,
	tx Execer,
	generations Generations,
) (Counts, error) {
	var counts Counts

	for _, step := range []struct {
		name     string
		template string
		target   *int
	}{
		{"delete succeeded reducer work", deleteSucceededReducerWorkTemplate, &counts.ReducerWorkDeleted},
		{"reopen shared projection intents", reopenSharedIntentsTemplate, &counts.SharedIntentsReopened},
		{"clear readiness phase state", clearReadinessPhaseStateTemplate, &counts.ReadinessPhasesCleared},
		{"retire resolution generations", retireResolutionGenerationsTemplate, &counts.GenerationsRetired},
	} {
		query, args := buildResetQuery(step.template, generations)
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return Counts{}, fmt.Errorf("refinalize rebuild reset: %s: %w", step.name, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return Counts{}, fmt.Errorf("refinalize rebuild reset: %s rows affected: %w", step.name, err)
		}
		*step.target = int(affected)
	}

	return counts, nil
}
