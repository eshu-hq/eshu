// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

// reducerClaimFunnelSweepCTEs renders read-only stand-ins for the sweep CTEs
// the claim composes from supersedeInactiveReducerGenerationsCTE: the same
// locked-membership SELECT (no ORDER BY, no FOR NO KEY UPDATE), the same
// carrier CTE verbatim, and the same generations membership as a SELECT (no
// UPDATE) over the locked set. The CTE names match the claim's sweep CTEs,
// so `base` resolves the same exclusion set; the shared join, predicate,
// carrier-arm, carrier, and row-filter sources keep the two from drifting.
// The statement takes no row locks and modifies nothing: Postgres still
// executes every CTE it references, but these CTEs are pure SELECTs.
//
// Read-only membership can differ from the claim's EvalPlanQual set under a
// race (a row a concurrent claimer leases between the two statements is
// still counted here), which is the documented census approximation, not a
// second lock pass: there is no SKIP LOCKED wait and no lock to contend.
func reducerClaimFunnelSweepCTEs() string {
	// NOTE: the locked list carries stale.stage on top of the shared
	// columns: the claim's UPDATE rechecks stage against its own target
	// row, but this SELECT form needs the column to apply the shared row
	// filters verbatim. The claim's sweep text is untouched.
	return `superseded_stale_reducer_locked AS (
    ` + supersedeInactiveReducerLockedColumnsSQL + `,
           stale.stage
    FROM fact_work_items AS stale,
         ` + supersedeInactiveReducerGenerationsJoin + `
    WHERE (` + supersedeInactiveReducerGenerationsPredicate + `)
` + supersedeInactiveReducerCarrierArmSQL + `
),
` + supersededCrossplaneNewestCarrierCTE + `
` + reducerClaimFunnelGenerationsCTE()
}

// reducerClaimFunnelGenerationsCTE renders the read-only stand-in for the
// sweep's terminal UPDATE...RETURNING: the same row-filter membership as a
// SELECT over the locked set.
//
// AS MATERIALIZED is load-bearing: without it the planner inlines this
// SELECT under base's NOT EXISTS and replays the sweep per candidate row
// (measured 614 ms / 1.8 M buffer hits vs the claim's 114 ms on the
// stale-heavy fixture). The claim's UPDATE...RETURNING form is inherently
// evaluated once; this marker gives the read-only stand-in the same
// evaluate-once shape.
func reducerClaimFunnelGenerationsCTE() string {
	return `superseded_stale_reducer_generations AS MATERIALIZED (
    SELECT stale.work_item_id
    FROM superseded_stale_reducer_locked AS stale,
         ingestion_scopes AS scope
    WHERE ` + supersedeInactiveReducerUpdateRowFiltersSQL + `
)`
}

// reducerClaimBatchFunnelQuery replays the batch-claim candidate chain
// (reducerClaimBatchCandidateChainCTEs, the same Go source the shipped claim
// composes) and counts each funnel stage in one pass: candidates in, the five
// ordered stage drops, the pre-fence representative count, and the
// representatives after the fence. The live-lease fence, the batch-limit
// trim, and the lock-time race derive in Go: fence = pre_fence -
// representatives (the candidate CTE's only additional filter is the live
// fence), over_limit trims representatives past the batch limit, and
// lock_lost is the within-limit shortfall against the admitted batch.
//
// The text reuses the claim's $1/$2/$5/$6/$7 placeholders with the same
// meanings (now, domain filter, projector-drain gate, semantic done-count,
// semantic cap). $3/$4 (lease owner/expiry) are unreferenced by the funnel
// text; the call passes the claim's values so placeholder positions stay
// identical to the shared chain. It takes no locks, fetches no payload, and
// never feeds the admission CTEs, so the claimed set and order are
// untouched (the C-step-1 byte-identical downstream rule stands).
//
// The census runs BEFORE the claim statement on the same clock, so the
// counts describe the claim that just ran (pre-claim membership): exact in
// the uncontended case, approximate under a claimer storm where a
// concurrent claim lands between the two statements. The admitted batch
// plus the closed-reason drops sum to the candidate count when nothing
// races. A rolled-back claim emits nothing: the census is dropped with it.
var reducerClaimBatchFunnelQuery = `
-- batch_claim_funnel_counts (#10565 box 6): candidates in, representatives
-- after the fence, and the ordered stage counts drops derive from.
WITH ` + reducerClaimReadinessRequirementsCTE() + `,
` + reducerClaimFunnelSweepCTEs() + `,
` + reducerClaimBatchCandidateChainCTEs() + `
SELECT
    (SELECT count(*) FROM base) AS candidates,
    (SELECT count(*) FROM base WHERE NOT readiness_ok) AS not_ready,
    (SELECT count(*) FROM reps_ranked WHERE same_rn <> 1) AS not_representative,
    (SELECT count(*) FROM reps WHERE same_rn = 1 AND NOT projector_ok) AS projector_drain,
    (SELECT count(*) FROM reps WHERE same_rn = 1 AND projector_ok AND NOT semantic_ok) AS semantic_cap,
    (SELECT count(*) FROM reps WHERE same_rn = 1 AND projector_ok AND semantic_ok) AS pre_fence,
    (SELECT count(*) FROM candidate) AS representatives
-- $3/$4 (lease owner/expiry) are unreferenced by the counts above, but the
-- shared chain keeps the claim's $1..$7 numbering so the text stays verbatim.
-- This tautology types both placeholders: Postgres rejects parameters whose
-- type it cannot infer (42P18), and an unreferenced placeholder has no type.
-- It filters nothing: the bare SELECT returns one row unconditionally.
WHERE ($3::text IS NULL OR $3::text IS NOT NULL)
  AND ($4::timestamptz IS NULL OR $4::timestamptz IS NOT NULL)
`

// batchClaimFunnelCounts is one row of reducerClaimBatchFunnelQuery.
type batchClaimFunnelCounts struct {
	candidates        int64
	notReady          int64
	notRepresentative int64
	projectorDrain    int64
	semanticCap       int64
	preFence          int64
	representatives   int64
}

// captureClaimBatchFunnel runs the funnel census for an upcoming batch claim
// and returns the pre-claim stage counts. now is the claim's clock. The
// query is read-only (TestClaimBatchFunnelQueryIsReadOnly), so capturing
// before the claim takes no locks and changes nothing; it only observes.
// Telemetry is best-effort: a capture failure reports false and the claim
// skips the funnel without failing. Queues without instruments skip the
// funnel query entirely. ok is false when the census must not be emitted
// (no instruments, capture failure): emitClaimBatchFunnel drops it.
func (q ReducerQueue) captureClaimBatchFunnel(ctx context.Context, now time.Time) (batchClaimFunnelCounts, bool) {
	var empty batchClaimFunnelCounts
	inst := q.Instruments
	if inst == nil {
		return empty, false
	}
	if !q.FunnelTelemetryEnabled {
		return empty, false
	}
	if inst.BatchClaimCandidates == nil && inst.BatchClaimRepresentatives == nil && inst.BatchClaimDrops == nil {
		return empty, false
	}
	rows, err := q.database.QueryContext(
		ctx,
		reducerClaimBatchFunnelQuery,
		now,
		q.claimDomainFilters(),
		q.LeaseOwner,
		now.Add(q.LeaseDuration),
		q.RequireProjectorDrainBeforeClaim,
		q.ExpectedSourceLocalProjectors,
		q.semanticEntityClaimLimit(),
	)
	if err != nil {
		return empty, false
	}
	defer func() { _ = rows.Close() }()
	var funnel batchClaimFunnelCounts
	if !rows.Next() {
		return empty, false
	}
	if err := rows.Scan(
		&funnel.candidates,
		&funnel.notReady,
		&funnel.notRepresentative,
		&funnel.projectorDrain,
		&funnel.semanticCap,
		&funnel.preFence,
		&funnel.representatives,
	); err != nil {
		return empty, false
	}
	if err := rows.Err(); err != nil {
		return empty, false
	}
	return funnel, true
}

// emitClaimBatchFunnel records a captured pre-claim census against the batch
// the claim that followed it admitted. admitted is the claimed batch size
// and limit the normalized batch limit. The counts describe the claim that
// just ran (pre-claim membership), so the admitted batch plus the
// closed-reason drops reconcile exactly to the candidate count when nothing
// races between the two statements; a concurrent claim landing between them
// is the documented approximation. ok false drops the census.
func (q ReducerQueue) emitClaimBatchFunnel(ctx context.Context, funnel batchClaimFunnelCounts, ok bool, admitted, limit int) {
	if !ok {
		return
	}
	inst := q.Instruments
	if inst == nil {
		return
	}
	// live_lease_fence is the only candidate filter the counts query does
	// not name: representatives are the pre-fence set minus the fenced rows.
	fence := max(funnel.preFence-funnel.representatives, 0)
	over := max(funnel.representatives-int64(limit), 0)
	lost := max(min(funnel.representatives, int64(limit))-int64(admitted), 0)

	if inst.BatchClaimCandidates != nil {
		inst.BatchClaimCandidates.Record(ctx, funnel.candidates)
	}
	if inst.BatchClaimRepresentatives != nil {
		inst.BatchClaimRepresentatives.Record(ctx, funnel.representatives)
	}
	if inst.BatchClaimDrops == nil {
		return
	}
	drops := []struct {
		reason string
		count  int64
	}{
		{telemetry.BatchClaimDropNotReady, funnel.notReady},
		{telemetry.BatchClaimDropNotRepresentative, funnel.notRepresentative},
		{telemetry.BatchClaimDropProjectorDrain, funnel.projectorDrain},
		{telemetry.BatchClaimDropSemanticCap, funnel.semanticCap},
		{telemetry.BatchClaimDropLiveLeaseFence, fence},
		{telemetry.BatchClaimDropOverLimit, over},
		{telemetry.BatchClaimDropLockLost, lost},
	}
	for _, drop := range drops {
		if drop.count <= 0 {
			continue
		}
		inst.BatchClaimDrops.Add(ctx, drop.count, metric.WithAttributes(telemetry.AttrReason(drop.reason)))
	}
}
