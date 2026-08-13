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

// AffectedGenerationsSubquery selects the (scope_id, generation_id) pairs one
// refinalize covers. All three reset statements render this same subquery with
// the same scope predicate, so they cannot disagree about which generations are
// being rebuilt.
//
// The projector re-enqueue in the parent package does NOT embed this constant --
// it is an INSERT ... SELECT over the same table and carries its own copy of the
// FROM and the two scope guards. What the two sides genuinely share is
// ScopePredicate, so the scope FILTER cannot drift; the `active_generation_id IS
// NOT NULL AND status = 'active'` guards are duplicated text and have to be
// changed in both places together.
//
// The %s is the scope predicate: empty for the all-scopes disaster-recovery
// path, `AND scope.scope_id = ANY($N)` for an explicit scope list.
const AffectedGenerationsSubquery = `
    SELECT scope.scope_id, scope.active_generation_id
    FROM ingestion_scopes AS scope
    WHERE scope.active_generation_id IS NOT NULL
      AND scope.status = 'active'
      %s
`

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
  AND (scope_id, generation_id) IN (` + AffectedGenerationsSubquery + `)
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
  AND (scope_id, generation_id) IN (` + AffectedGenerationsSubquery + `)
`

// clearReadinessPhaseStateTemplate re-arms the readiness gates by deleting the
// phase rows that survived the graph wipe. The re-projection republishes each
// phase as it commits, so the gates hold work until its inputs genuinely exist
// rather than waving it through on an answer about a graph that is gone.
const clearReadinessPhaseStateTemplate = `
DELETE FROM graph_projection_phase_state
WHERE (scope_id, generation_id) IN (` + AffectedGenerationsSubquery + `)
`

// ScopePredicate renders the scope predicate shared by every statement in a
// refinalize, plus the args that fill it. placeholder is the $N to use, which
// differs per statement because the projector insert carries a leading timestamp
// argument and the resets do not.
//
// The all-scopes path drops the clause rather than passing an empty array,
// because `scope_id = ANY('{}')` matches no rows: a rebuild that silently
// selected nothing would report success and leave the graph empty.
func ScopePredicate(filter recovery.RefinalizeFilter, placeholder int) (string, []any) {
	if filter.AllScopes {
		return "", nil
	}

	return fmt.Sprintf("AND scope.scope_id = ANY($%d)", placeholder), []any{filter.ScopeIDs}
}

// buildResetQuery renders one reset statement for the filter's scopes. The reset
// statements take no timestamp, so their scope predicate starts at $1.
func buildResetQuery(template string, filter recovery.RefinalizeFilter) (string, []any) {
	predicate, args := ScopePredicate(filter, 1)
	return fmt.Sprintf(template, predicate), args
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
}

// Apply clears the three pieces of dedup state that would otherwise make a
// rebuild-from-facts stop at source-local structure. It runs inside the caller's
// transaction so a refinalize either re-enqueues the projector work and reopens
// its downstream state together, or does neither.
//
// All three statements touch terminal state only, so no live lease is taken away
// and no claimed item can double-execute.
func Apply(
	ctx context.Context,
	tx Execer,
	filter recovery.RefinalizeFilter,
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
	} {
		query, args := buildResetQuery(step.template, filter)
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
