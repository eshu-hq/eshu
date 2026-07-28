// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"
	"time"
)

// containerImageIdentityRetireQuery removes this domain's decisions for one
// (scope, generation) that are not in the freshly written set, mirroring
// eshuSearchDocumentRetireQuery and its aws_cloud_runtime_drift sibling. It is
// what makes replaying a succeeded container_image_identity intent idempotent
// (#5847).
//
// # Why a retire is needed at all
//
// The decision identity embeds `outcome` — directly in
// containerImageIdentityIdentity, and again through
// containerImageIdentityStableFactKey and the canonical_id derived from it — as
// well as `image_ref`, which the exact-digest branch REWRITES to the resolved
// OCI repository form (classifyContainerImageRef, via
// imageRefFromOCIRepositoryID). A replay that re-classifies an image therefore
// writes a NEW fact_id, and the bulk insert conflicts only on fact_id, so the
// superseded row would otherwise stay live (is_tombstone = false, same active
// generation). PostgresContainerImageIdentityStore.ListContainerImageIdentities
// has no DISTINCT ON, no GROUP BY, and no per-digest latest-wins — it filters on
// is_tombstone plus the active-generation join and orders by fact_id — so both
// rows are served, and the aggregate rollups in
// go/internal/query/container_image_identity_aggregates.go count both.
//
// container_image_identity sits in the bootstrap reopen slice
// (go/cmd/bootstrap-index/bootstrap_pipeline.go) precisely because a replay is
// expected once the cross-scope OCI generation activates, so re-classification
// is the ordinary path here. Two replays need this:
//
//   - a decision whose outcome CHANGES between the two durably-written
//     outcomes, exact_digest and tag_resolved — the only two that set
//     CanonicalWrites=1 (container_image_identity_registry.go);
//   - a decision DEMOTED out of the canonical set entirely: a digest that
//     matched exactly one registry observation resolves to exact_digest, and
//     falls to ambiguous_tag once a second cross-scope OCI generation activates
//     with another repository observing the same digest. No candidate row is
//     produced at all, so an identity-keyed upsert writes nothing over the stale
//     row. Only an empty keep-set clears it, which is why an empty write still
//     retires.
//
// # Why the retire is scoped to the whole generation
//
// One intent covers one whole scope generation:
// buildContainerImageIdentityReducerIntent emits a single intent keyed
// "container_image_identity:<scope>"
// (go/internal/projector/container_image_identity_intents.go), the handler calls
// the writer exactly once per intent, and the evidence loader pages the active
// fact set to exhaustion rather than truncating at the page limit
// (go/internal/storage/postgres/facts_active_container_image_identity.go). There
// is no path that evaluates a SUBSET of the generation's image references, so a
// row absent from the write set is genuinely no longer a decision. This fact
// kind is also the only one this domain writes durably — the provenance and
// base-image projections emit graph edges, not fact records — so nothing else
// under this (scope, generation) is in the retire's blast radius. A future
// change that made the handler write a second fact kind here, or evaluate only
// part of a generation, would break that invariant and must revisit this retire.
//
// # Why the retire is fenced ($5)
//
// The generation scope above bounds WHICH rows this statement may touch. It does
// not bound WHO may touch them, and this delete is authoritative enough that the
// difference matters.
//
// The reducer queue leases with an expiry, and heartbeat loss is quarantined
// only AFTER Handle has already returned (reducer/service.go) — so a worker
// whose lease lapsed mid-execution still completes its write. That is the
// classic stalled-holder shape: worker A loads evidence, stalls past its lease,
// worker B reclaims the item, loads FRESHER evidence and writes, and then A
// lands. Before the retire existed, A merely left a wrong row beside B's correct
// one. An unfenced generation-authoritative retire is strictly worse: A would
// DELETE B's correct row.
//
// The claim batch's in-flight exclusion does NOT cover this. That exclusion
// (go/internal/storage/postgres/reducer_queue_batch_query.go) requires
// inflight.claim_until > now — a LIVE lease — while the base predicate re-admits
// an item whose claim_until has already passed. Lease expiry IS the
// stalled-worker case, so the queue fence excludes every same-scope overlap
// EXCEPT the hazard. eshuSearchDocumentRetireQuery does not rely on it either:
// it has the #4233 invalidate-before-mutate ProjectionState fence (BeginBuilding
// returning a revision and a fence, eshu_search_document_writer.go). This domain
// has no ProjectionState at all, so it needs its own.
//
// $5 is a fencing token derived from when the write's evidence was READ
// (ContainerImageIdentityWrite.EvidenceAsOf, captured by the handler immediately
// before its first fact load), not from when the write landed. Write time ranks
// the stalled worker highest, which is backwards; evidence-read time ranks the
// worker holding the stale view lowest, which is the whole point. The DELETE
// removes only rows at or below this write's token, so rows written from fresher
// evidence than ours survive us.
//
// # Why the retire does NOT also stamp the keep-set
//
// An earlier shape led with a `WITH stamped AS (UPDATE ... SET fencing_token =
// $5 ... WHERE fact_id = ANY($4) AND fencing_token <= $5)` CTE, on the reasoning
// that the partition should carry a durable record of how fresh each row's
// evidence was. It does — but the INSERT already supplies it. reducerFactRow
// carries FencingToken and reducerFactBatchInsertQuery binds it under a conflict
// guard that rejects any upsert from a lower token, so both properties the CTE
// was there for come from the insert: the row is stamped, and a stale pass
// cannot downgrade a fresher row's token.
//
// That left the CTE a proven no-op that still cost a write. keepFactIDs is built
// from the exact rows just handed to reducerBatchInsertFacts, so by retire time
// every keep-set row already carries `fencing_token >= $5`; the only rows
// `fencing_token <= $5` can still match are the ones sitting at exactly `$5`,
// which the UPDATE then sets to `$5`. Postgres has no in-place UPDATE, so each
// match wrote a SECOND row version per canonical decision per intent execution —
// doubled WAL, dead tuples, and vacuum pressure on this domain's hot write path
// — while the committed cost budget, which counts STATEMENTS, stayed at two and
// saw none of it. Measured on Postgres 16: keep-set `xmin` moved 879 -> 880 with
// `fencing_token` unchanged. TestContainerImageIdentityRetireDoesNotRewriteKeep
// SetRowsLive counts row versions rather than statements and is red on the CTE
// shape.
//
// Dropping it also removes a lock phase. The CTE took row locks over the
// keep-set while the DELETE took them over the complement, and Postgres does not
// specify sub-statement ordering within a `WITH`. Two concurrent same-scope
// retires with crossed keep/delete sets — r1 in keepA and deleteB, r2 in keepB
// and deleteA, which is exactly the stalled-worker shape this fence exists for —
// could therefore deadlock ABBA. A single DELETE scanning one index order
// cannot.
//
// The token is a wall-clock microsecond reading, so it is monotonic across
// reopens and retries without needing a durable counter — unlike the queue's
// attempt_count, which the reopen-succeeded statement deliberately resets to 0
// and which therefore cannot fence a reopened replay against the run it is
// repairing. Two reducer processes read their own clocks, so the ordering is
// only as good as NTP between them; the hazard window is a whole lease duration,
// which is orders of magnitude larger than realistic host clock skew.
//
// # What this fence does NOT close
//
// A stale pass can still INSERT. If A's insert lands after B's retire has
// already run, A's stale row is added under its own fact_id with the default
// token 0, and no later pass in that generation removes it — leaving the
// two-contradictory-rows shape that exists on main today. Closing that needs a
// begin-before-mutate projection-state fence (the #4233
// ProjectionState.BeginBuilding pattern eshu_search_document_writer.go uses) so
// a stale pass is rejected before it writes anything, which is a larger change
// than this repair. This statement is what stops the retire from making that
// case WORSE by deleting the correct row.
//
// Nor does the fence help a pass that read EMPTY evidence: that pass read LAST,
// so it ranks highest, and its empty keep-set retires the whole partition. The
// writer flags that shape instead — see
// ContainerImageIdentityWriteResult.RetiredWithoutCanonicalWrites.
const containerImageIdentityRetireQuery = `
DELETE FROM fact_records
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND fact_id <> ALL($4::text[])
  AND fencing_token <= $5
`

// errContainerImageIdentityMissingEvidenceAsOf is returned when a write reaches
// the writer without the evidence-read watermark the retire fence ranks by.
var errContainerImageIdentityMissingEvidenceAsOf = errors.New(
	"container image identity write requires evidence_as_of: the retire fence has no watermark to compare against",
)

// containerImageIdentityFencingToken renders the write's evidence-read watermark
// as the BIGINT fact_records.fencing_token carries.
//
// Microsecond resolution matches Postgres' own timestamp resolution and leaves
// int64 headroom for ~294,000 years, so no saturation handling is needed.
func containerImageIdentityFencingToken(write ContainerImageIdentityWrite) int64 {
	return write.EvidenceAsOf.UTC().UnixMicro()
}

// validateContainerImageIdentityFence rejects a write with no evidence-read
// watermark.
//
// This is deliberately a hard error rather than a defaulted value. A zero
// EvidenceAsOf yields token 0, and rows carry 0 by table default, so
// `fencing_token <= 0` would still match everything: the retire would run
// completely UNFENCED and nothing would say so. Defaulting the watermark to the
// writer's own clock would be worse, because write time ranks a stalled worker
// highest — the exact inversion the fence exists to prevent.
func validateContainerImageIdentityFence(write ContainerImageIdentityWrite) error {
	if write.EvidenceAsOf.IsZero() {
		return errContainerImageIdentityMissingEvidenceAsOf
	}
	return nil
}

// containerImageIdentityEvidenceAsOf reads the handler's clock for the
// evidence-read watermark, falling back to the process clock when the handler
// left Now unset.
func containerImageIdentityEvidenceAsOf(now func() time.Time) time.Time {
	return reducerWriterNow(now)
}
