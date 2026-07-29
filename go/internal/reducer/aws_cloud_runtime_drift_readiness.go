// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// awsCloudRuntimeDriftStatePendingMaxWait bounds the readiness defer (#5848,
// #5837's root cause) by ELAPSED TIME SINCE THE CURRENT REPAIR CYCLE BEGAN
// (Intent.CycleStartedAt, falling back to Intent.EnqueuedAt -- see
// shouldDeferForStatePending), not by retry count. Below the bound, an
// orphaned_cloud_resource candidate is held back while a Terraform
// state_snapshot scope is still mid-ingestion, because it might supply the
// state that would reclassify the finding. At or past the bound, Handle
// commits its best-available classification instead of deferring forever —
// the terminal fallback a genuine orphan (no Terraform anywhere, ever) needs,
// since a permanently absent (or permanently stuck) state scope would
// otherwise starve the intent.
//
// This MUST be time-based, not a hand-set Intent.AttemptCount comparison
// (that was the original, wrong shape — see the correction below).
// Intent.AttemptCount comes from fact_work_items.attempt_count, which
// reducerClaimAttemptCountCaseSQL FREEZES for any failure_class enrolled in
// nonCountingReducerRetryFailureClasses — exactly the class this defer
// self-reports (AWSCloudRuntimeDriftStatePendingFailureClass), by design, so
// the SAME retry never dead-letters. That freeze means AttemptCount stops
// advancing after the FIRST defer and Handle sees the identical value on
// every subsequent claim forever: a bound compared against it can never fire.
// Proven live in TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOver
// RealQueueLive (go/internal/storage/postgres), which drives the REAL
// ReducerQueue through repeated claim/fail cycles and asserts the domain
// eventually commits.
//
// It also MUST NOT be anchored on Intent.EnqueuedAt alone (a second, distinct
// mistake this doc comment used to make, found by a round-3 review after the
// AttemptCount fix above landed): EnqueuedAt is populated from
// fact_work_items.created_at, which is immutable for the life of the row —
// including across a reopen. aws_cloud_runtime_drift is unconditionally
// reopened on every ingester shard drain and bootstrap maintenance pass
// (postgres.CrossScopeCorrelationReopenDomains), so for essentially every
// maintenance-triggered reopen in a real deployment, more than this bound has
// already elapsed since the row's ORIGINAL enqueue. Anchored on EnqueuedAt
// alone, shouldDeferForStatePending would return false on the very first
// claim of the reopened row WITHOUT EVER CONSULTING THE READINESS CHECKER,
// committing a degraded verdict immediately even though the state_snapshot
// scope is freshly, legitimately still pending for this repair cycle — the
// exact no-grace-window regression the old AttemptCount=0 reset on reopen
// (see ReopenSucceeded/ReplayDomain in
// go/internal/storage/postgres/reducer_queue_replay.go) used to prevent,
// before its own freeze bug (the paragraph above) made it not actually work.
//
// Intent.CycleStartedAt closes that gap without reintroducing the freeze:
// ReopenSucceeded/ReplayDomain reset fact_work_items.reopened_at to the
// reopen timestamp in the SAME statement that resets attempt_count to 0
// (migration 088), so a reopened row's CycleStartedAt
// (COALESCE(reopened_at, created_at), computed in the claim query) becomes
// the reopen time, giving this bound a genuinely fresh window. Ordinary
// claim/retry/fail never touch reopened_at (see reducer_queue.go's
// retryReducerWorkQuery and reducer_queue_claim_query.go's claim CASE), so it
// stays just as freeze-immune as created_at always was — proven live by
// TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive
// (go/internal/storage/postgres), which reopens a row against real Postgres
// after its original EnqueuedAt is already past the bound and asserts the
// reopened claim still defers instead of committing immediately.
//
// "How long have we been waiting for a sibling scope to activate, in THIS
// repair cycle" is a time concept, not a retry-count concept, so bounding it
// on elapsed wall-clock time is also the more honest match for what is
// actually being bounded.
//
// 30 minutes is a judgment call, not a measured threshold: generous enough
// that ordinary asynchronous Terraform-state ingestion (observed on the order
// of single-digit minutes elsewhere in this codebase's local proofs) never
// hits it, bounded enough that a genuinely stuck or permanently-absent state
// scope converges to a terminal verdict well within an operator's normal
// triage window rather than accumulating unbounded stale-orphan noise.
const awsCloudRuntimeDriftStatePendingMaxWait = 30 * time.Minute

// AWSCloudRuntimeDriftReadinessChecker reports whether a Terraform
// state_snapshot scope is still mid-ingestion anywhere, so the handler can
// tell "not ready" apart from "verdict" for an orphaned_cloud_resource
// classification.
//
// This is coarse by design: it does not attempt to prove that a SPECIFIC
// pending scope would resolve a SPECIFIC ARN, because which backend/locator
// owns a given ARN cannot be known until state activates and the ARN join
// resolves (the same chicken-and-egg the config-owner resolution already
// lives with, see aws_cloud_runtime_drift_evidence.go's
// AWSCloudRuntimeDriftConfigResolver). The checker only tells Handle that AT
// LEAST ONE state_snapshot scope has not yet finished, which can only ever
// cause an UNNECESSARY defer (bounded by awsCloudRuntimeDriftStatePendingMaxWait,
// an ELAPSED-TIME bound -- see that constant's doc comment for why it cannot
// be a retry-count bound), never an incorrect verdict.
//
// This is deliberately its OWN mechanism rather than an entry in
// crossScopeDependencyCatalog (cross_scope_dependencies.go): that catalog's
// CrossScopeDependency.ProducerDomains names REDUCER DOMAINS, and the producer
// here is raw Terraform-state collector evidence in ANY state_snapshot:* scope
// -- not a reducer domain's canonical output. Generalizing the catalog to
// express a scope-shaped producer is the #5709 follow-up the issue asked to be
// scoped with that surface's owner; this checker does not attempt it.
type AWSCloudRuntimeDriftReadinessChecker interface {
	// HasPendingStateSnapshotEvidence reports whether any state_snapshot:*
	// ingestion scope has a generation still in status 'pending' -- registered
	// and mid-ingestion, neither active nor failed.
	HasPendingStateSnapshotEvidence(ctx context.Context) (bool, error)
}

// AWSCloudRuntimeDriftStatePendingFailureClass classifies a Handle call
// deferred because a Terraform state_snapshot scope has not finished
// ingesting. The reducer queue treats it as a non-counting retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go) so the defer
// never erodes the retry budget while the intent waits for state to activate.
const AWSCloudRuntimeDriftStatePendingFailureClass = "aws_cloud_runtime_drift_state_pending"

// awsCloudRuntimeDriftStatePendingError marks a readiness-gate defer as
// retryable so the durable queue re-runs the intent once the pending
// state_snapshot scope activates (or the bound is reached and Handle commits
// its best-available classification instead).
type awsCloudRuntimeDriftStatePendingError struct {
	scopeID      string
	generationID string
}

func newAWSCloudRuntimeDriftStatePendingError(scopeID, generationID string) error {
	return awsCloudRuntimeDriftStatePendingError{scopeID: scopeID, generationID: generationID}
}

func (e awsCloudRuntimeDriftStatePendingError) Error() string {
	return fmt.Sprintf(
		"aws cloud runtime drift deferred for scope %s generation %s: a terraform state_snapshot scope is still mid-ingestion",
		e.scopeID, e.generationID,
	)
}

func (awsCloudRuntimeDriftStatePendingError) Retryable() bool { return true }

func (awsCloudRuntimeDriftStatePendingError) FailureClass() string {
	return AWSCloudRuntimeDriftStatePendingFailureClass
}

// shouldDeferForStatePending reports whether Handle must defer instead of
// writing, bounded by awsCloudRuntimeDriftStatePendingMaxWait elapsed since
// the current repair cycle began (NOT intent.AttemptCount -- see that
// constant's doc comment for why a retry-count bound can never fire once this
// defer's own failure class freezes the queue's attempt_count). now must be
// the SAME clock reading Handle uses for its evidence-read watermark, passed
// in rather than read again here, so a slow evidence load cannot itself push
// the intent past the bound. Returns false (never defer, so Handle proceeds
// to write) when the checker is nil (gate disabled, matches pre-#5848
// behavior), when the elapsed-time bound is reached (terminal fallback), or
// when no admitted candidate is actually an orphaned_cloud_resource (a
// pending state scope cannot improve any other finding kind:
// unmanaged/ambiguous/unknown/image_version_drift all already require state
// to be present).
//
// The anchor is intent.CycleStartedAt when set, falling back to
// intent.EnqueuedAt otherwise. CycleStartedAt is what the real queue always
// populates (COALESCE(reopened_at, created_at) in the claim query) and is the
// only one of the two that gets a fresh value on reopen/replay -- see
// awsCloudRuntimeDriftStatePendingMaxWait's doc comment for why anchoring on
// EnqueuedAt alone regressed the reopen grace window a round-3 review caught.
// The EnqueuedAt fallback exists only for a hand-built Intent (a test or a
// future caller) that does not populate CycleStartedAt; it never fires
// against the real queue, which always returns both fields.
//
// A zero anchor (neither field set -- should not happen against the real
// queue, but reachable from a hand-built Intent in a test) is treated as
// "elapsed time unknown" rather than "infinitely elapsed": subtracting a zero
// time.Time from now would otherwise read as tens of thousands of hours and
// fire the terminal fallback immediately on the very first defer. Failing
// toward "keep deferring" is the safe direction here -- it costs a bounded
// delay, where failing toward "commit now" on a misread zero timestamp would
// write a possibly-wrong verdict for a reason unrelated to actual elapsed
// time.
func (h AWSCloudRuntimeDriftHandler) shouldDeferForStatePending(
	ctx context.Context,
	intent Intent,
	admitted []model.Candidate,
	now time.Time,
) (bool, error) {
	if h.ReadinessChecker == nil {
		return false, nil
	}
	anchor := awsCloudRuntimeDriftCycleAnchor(intent)
	if !anchor.IsZero() && now.Sub(anchor) >= awsCloudRuntimeDriftStatePendingMaxWait {
		return false, nil
	}
	if !hasOrphanedAWSCloudRuntimeDriftCandidate(admitted) {
		return false, nil
	}
	pending, err := h.ReadinessChecker.HasPendingStateSnapshotEvidence(ctx)
	if err != nil {
		return false, err
	}
	return pending, nil
}

// awsCloudRuntimeDriftCycleAnchor returns intent.CycleStartedAt when set,
// falling back to intent.EnqueuedAt -- see shouldDeferForStatePending's doc
// comment for why CycleStartedAt is the correct anchor and EnqueuedAt is only
// a fallback for callers that do not populate it.
func awsCloudRuntimeDriftCycleAnchor(intent Intent) time.Time {
	if !intent.CycleStartedAt.IsZero() {
		return intent.CycleStartedAt
	}
	return intent.EnqueuedAt
}

// hasOrphanedAWSCloudRuntimeDriftCandidate reports whether any admitted
// candidate classified as orphaned_cloud_resource -- the only finding kind a
// not-yet-activated Terraform state scope could still improve.
func hasOrphanedAWSCloudRuntimeDriftCandidate(admitted []model.Candidate) bool {
	for _, candidate := range admitted {
		if cloudruntime.FindingKind(awsCloudRuntimeFindingKind(candidate)) == cloudruntime.FindingKindOrphanedCloudResource {
			return true
		}
	}
	return false
}

// evaluatedAWSCloudRuntimeDriftARNs returns every ARN this pass's evidence
// load covered, deduplicated and independent of whether Classify produced an
// admitted candidate for it. The generation-authoritative retire uses this to
// bound its blast radius (#5848).
func evaluatedAWSCloudRuntimeDriftARNs(rows []cloudruntime.AddressedRow) []string {
	seen := make(map[string]struct{}, len(rows))
	arns := make([]string, 0, len(rows))
	for _, row := range rows {
		arn := strings.TrimSpace(row.ARN)
		if arn == "" {
			continue
		}
		if _, ok := seen[arn]; ok {
			continue
		}
		seen[arn] = struct{}{}
		arns = append(arns, arn)
	}
	return arns
}

// isAWSCloudRuntimeDriftWriteSuperseded reports whether err is (or wraps) the
// insert-admission rejection, so Handle can log it distinctly from a handler
// error.
func isAWSCloudRuntimeDriftWriteSuperseded(err error) bool {
	var superseded awsCloudRuntimeDriftWriteSupersededError
	return errors.As(err, &superseded)
}

// logStatePendingDefer records a readiness-gate defer as its own structured
// log line, distinct from an admitted-findings log or a handler error, so an
// operator can tell the three apart (#5848 acceptance criterion). Logs
// elapsed time against the bound, not attempt_count: attempt_count is frozen
// by this defer's own non-counting failure class (see
// awsCloudRuntimeDriftStatePendingMaxWait's doc comment) and would read as a
// constant on every occurrence, telling an operator nothing about how close
// the intent is to its terminal fallback.
func (h AWSCloudRuntimeDriftHandler) logStatePendingDefer(
	ctx context.Context,
	intent Intent,
	admitted []model.Candidate,
	now time.Time,
) {
	if h.Logger == nil {
		return
	}
	attrs := []slog.Attr{
		log.Domain(string(intent.Domain)),
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("admitted_candidates", len(admitted)),
		slog.Duration("max_wait", awsCloudRuntimeDriftStatePendingMaxWait),
	}
	if anchor := awsCloudRuntimeDriftCycleAnchor(intent); !anchor.IsZero() {
		attrs = append(attrs, slog.Duration("elapsed_since_cycle_start", now.Sub(anchor)))
	}
	h.Logger.LogAttrs(ctx, slog.LevelInfo, "aws cloud runtime drift deferred for pending terraform state", attrs...)
}

// logWriteSuperseded records an insert-admission rejection as its own
// structured log line, distinct from a readiness defer or a handler error.
func (h AWSCloudRuntimeDriftHandler) logWriteSuperseded(ctx context.Context, intent Intent) {
	if h.Logger == nil {
		return
	}
	h.Logger.LogAttrs(
		ctx, slog.LevelInfo, "aws cloud runtime drift write superseded by a fresher pass",
		log.Domain(string(intent.Domain)),
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("attempt_count", intent.AttemptCount),
	)
}
