// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

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
// Counts are a second snapshot after the claim commits: exact in the
// uncontended case, approximate under a claimer storm where a concurrent
// claim lands between the two statements. Drops sum with the admitted batch
// to the candidate count when nothing races.
var reducerClaimBatchFunnelQuery = `
-- batch_claim_funnel_counts (#10565 box 6): candidates in, representatives
-- after the fence, and the ordered stage counts drops derive from.
WITH ` + reducerClaimReadinessRequirementsCTE() + `,
` + supersedeInactiveReducerGenerationsCTE + `,
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

// recordClaimBatchFunnel runs the funnel census for a completed batch claim
// and records candidates in, representatives after the fence, and drops by
// closed reason. now is the claim's clock, admitted the claimed batch size,
// and limit the normalized batch limit. Telemetry is best-effort: a funnel
// failure records nothing and never fails the claim it observes. Queues
// without instruments skip the funnel query entirely.
func (q ReducerQueue) recordClaimBatchFunnel(ctx context.Context, now time.Time, admitted, limit int) {
	inst := q.Instruments
	if inst == nil {
		return
	}
	if inst.BatchClaimCandidates == nil && inst.BatchClaimRepresentatives == nil && inst.BatchClaimDrops == nil {
		return
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
		return
	}
	defer func() { _ = rows.Close() }()
	var funnel batchClaimFunnelCounts
	if !rows.Next() {
		return
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
		return
	}
	if err := rows.Err(); err != nil {
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
