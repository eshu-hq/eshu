// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// Disaster-recovery rebuild reset (#4594).
//
// Eshu's graph is a projection: throw it away, keep Postgres, and a refinalize
// replays every active generation to rebuild it. That worked for source-local
// structure and stopped there, because three pieces of Postgres state outlive a
// graph wipe and each one tells the pipeline the work is already done.
//
//  1. Succeeded reducer work items. A re-projection re-derives every reducer
//     intent, but the enqueue is ON CONFLICT (work_item_id) DO NOTHING and the
//     ids are stable, so each one collides with its succeeded row and is dropped.
//     The projector's intent_enqueue log line reports enqueued_count 0 and the
//     domain never runs again.
//  2. Shared projection intents with completed_at set. The partition workers
//     drain only completed_at IS NULL, and the upsert's COALESCE deliberately
//     refuses to reopen a completed row.
//  3. Graph projection phase rows, which assert that canonical nodes are
//     committed. After a wipe that assertion is false, and it is the worst kind
//     of false: the edge Cypher is MATCH-only, so work admitted on a stale
//     readiness answer matches nothing, writes nothing, and still acks succeeded.
//
// Both dedup guards stay exactly as they are. They are correct for ordinary
// operation, where every shard drain, reopen, and retry depends on completed
// work staying completed. What this file adds is a reset scoped to the
// generations a refinalize is actually rebuilding, issued once by the recovery
// path, in the same transaction as the projector re-enqueue.
//
// Why the reducer rows are DELETED rather than reset to pending: a pending row
// is claimable immediately, before the projector re-run that owns its inputs has
// committed anything. In ordinary operation a reducer intent exists only because
// the projector already emitted it, after the canonical node write; resetting to
// pending breaks that causality and lets a handler write into a wiped graph and
// ack succeeded, which is the same silent-incompleteness defect this change
// exists to fix. Deleting restores first-ingest semantics: the work exists again
// only once its producer has run. It also avoids a second trap -- reset-to-pending
// violates fact_work_items_container_image_identity_v2_status_check, which ties
// status to container_image_identity_v2_authorized_status, so a blind status
// rewrite fails on exactly the rows that carry that coupled column family.

// refinalizeAffectedGenerationsSubquery selects the (scope_id, generation_id)
// pairs one refinalize covers. Every statement in the rebuild renders this same
// subquery with the same scope predicate, so the projector re-enqueue and the
// three resets cannot disagree about which generations are being rebuilt. The
// %s is the scope predicate: empty for the all-scopes disaster-recovery path,
// `AND scope.scope_id = ANY($N)` for an explicit scope list.
const refinalizeAffectedGenerationsSubquery = `
    SELECT scope.scope_id, scope.active_generation_id
    FROM ingestion_scopes AS scope
    WHERE scope.active_generation_id IS NOT NULL
      AND scope.status = 'active'
      %s
`

// refinalizeDeleteSucceededReducerWorkTemplate removes the succeeded reducer work
// items whose existence would otherwise make the re-projection's enqueue a no-op.
//
// Scoped to 'succeeded' on purpose. Claimed and running rows hold live leases
// that a rebuild must not yank; dead_letter and failed rows belong to the replay
// endpoint and contributed nothing to the pre-wipe graph, so leaving them alone
// costs the rebuild no truth.
const refinalizeDeleteSucceededReducerWorkTemplate = `
DELETE FROM fact_work_items
WHERE stage = 'reducer'
  AND status = 'succeeded'
  AND (scope_id, generation_id) IN (` + refinalizeAffectedGenerationsSubquery + `)
`

// refinalizeReopenSharedIntentsTemplate clears completed_at so the partition
// workers drain these intents again.
//
// Cleared, not deleted: the payload is the drain's input, and a reducer domain
// that does not re-emit an identical intent this time round would otherwise lose
// its edges entirely. The `completed_at IS NOT NULL` guard makes the reported
// count mean "reopened" rather than "matched", so a second refinalize over the
// same generations reports zero instead of re-counting its own work.
const refinalizeReopenSharedIntentsTemplate = `
UPDATE shared_projection_intents
SET completed_at = NULL
WHERE completed_at IS NOT NULL
  AND (scope_id, generation_id) IN (` + refinalizeAffectedGenerationsSubquery + `)
`

// refinalizeClearReadinessPhaseStateTemplate re-arms the readiness gates by
// deleting the phase rows that survived the graph wipe. The re-projection
// republishes each phase as it commits, so the gates hold work until its inputs
// genuinely exist rather than waving it through on an answer about a graph that
// is gone.
const refinalizeClearReadinessPhaseStateTemplate = `
DELETE FROM graph_projection_phase_state
WHERE (scope_id, generation_id) IN (` + refinalizeAffectedGenerationsSubquery + `)
`

// refinalizeScopePredicate renders the scope predicate shared by every statement
// in a refinalize, plus the args that fill it. placeholder is the $N to use,
// which differs per statement because the projector insert carries a leading
// timestamp argument and the resets do not.
//
// The all-scopes path drops the clause rather than passing an empty array,
// because `scope_id = ANY('{}')` matches no rows: a rebuild that silently
// selected nothing would report success and leave the graph empty.
func refinalizeScopePredicate(filter recovery.RefinalizeFilter, placeholder int) (string, []any) {
	if filter.AllScopes {
		return "", nil
	}

	return fmt.Sprintf("AND scope.scope_id = ANY($%d)", placeholder), []any{filter.ScopeIDs}
}

// buildRefinalizeResetQuery renders one reset statement for the filter's scopes.
// The reset statements take no timestamp, so their scope predicate starts at $1.
func buildRefinalizeResetQuery(template string, filter recovery.RefinalizeFilter) (string, []any) {
	predicate, args := refinalizeScopePredicate(filter, 1)
	return fmt.Sprintf(template, predicate), args
}

// refinalizeRebuildResetCounts reports how much dedup state one refinalize
// cleared. An operator running a recovery needs these to distinguish "the
// rebuild re-queued the scopes" from "the rebuild will actually rebuild the
// whole graph"; before this existed, the two were indistinguishable until the
// graph came back short.
type refinalizeRebuildResetCounts struct {
	reducerWorkDeleted     int
	sharedIntentsReopened  int
	readinessPhasesCleared int
}

// applyRefinalizeRebuildReset clears the three pieces of dedup state that would
// otherwise make a rebuild-from-facts stop at source-local structure. It runs
// inside the caller's transaction so a refinalize either re-enqueues the
// projector work and reopens its downstream state together, or does neither.
//
// All three statements touch terminal state only, so no live lease is taken away
// and no claimed item can double-execute.
func applyRefinalizeRebuildReset(
	ctx context.Context,
	tx Transaction,
	filter recovery.RefinalizeFilter,
) (refinalizeRebuildResetCounts, error) {
	var counts refinalizeRebuildResetCounts

	for _, step := range []struct {
		name     string
		template string
		target   *int
	}{
		{"delete succeeded reducer work", refinalizeDeleteSucceededReducerWorkTemplate, &counts.reducerWorkDeleted},
		{"reopen shared projection intents", refinalizeReopenSharedIntentsTemplate, &counts.sharedIntentsReopened},
		{"clear readiness phase state", refinalizeClearReadinessPhaseStateTemplate, &counts.readinessPhasesCleared},
	} {
		query, args := buildRefinalizeResetQuery(step.template, filter)
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return refinalizeRebuildResetCounts{}, fmt.Errorf("refinalize rebuild reset: %s: %w", step.name, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return refinalizeRebuildResetCounts{}, fmt.Errorf("refinalize rebuild reset: %s rows affected: %w", step.name, err)
		}
		*step.target = int(affected)
	}

	return counts, nil
}
