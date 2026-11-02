// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// DefaultClaimFenceMargin is how far the latest claim-plane pin deadline must
// exceed the store clock when COMMIT is sent (#9376 D6). It must cover twice
// the clock skew between collector and claim plane plus the commit p99, and
// stay below two thirds of the claim lease so a healthy re-pin leaves slack.
const DefaultClaimFenceMargin = 5 * time.Second

// DefaultClaimFencePinAttemptTimeout bounds one pin or re-pin call (#9376
// D7), capped at a third of the claim lease so a hung call never outlasts a
// re-pin interval. It matches the 10 s http.Client timeout the claim-plane
// client wiring sets; whichever fires first, a hung plane is a failed attempt.
const DefaultClaimFencePinAttemptTimeout = 10 * time.Second

// Pin retry bounds for the pre-transaction pin (#9376 D7).
const (
	claimFencePinAttempts = 3
	claimFencePinBackoff  = 200 * time.Millisecond
)

// ClaimPlaneFence switches the claimed commit paths of an IngestionStore from
// the in-tree fence (HeartbeatClaim on the fact transaction) to the
// cross-service commit fence for claims held by the extracted claim plane
// (#9376). A nil *ClaimPlaneFence on the store keeps the in-tree path exactly
// as it was. The same wiring that hands a collector a claim-plane
// ControlStore must set this with that client as the Pinner; a plane-held
// claim committed through the in-tree path fails its core heartbeat.
type ClaimPlaneFence struct {
	// Pinner pins the claim on the plane; *workflow.ClaimPlaneClient
	// satisfies it. Required.
	Pinner fence.Pinner
	// Code maps a Pinner error to its protocol code. Nil uses
	// workflow.ClaimPlaneErrorCode.
	Code fence.CodeFunc
	// Tracer, when set, records the ingestion.commit.claim_fence span.
	Tracer trace.Tracer
	// Margin overrides DefaultClaimFenceMargin when positive.
	Margin time.Duration
	// Ticks overrides the re-pin ticker (tests). Nil uses a time.Ticker at
	// the claim lease divided by three.
	Ticks func(interval time.Duration) (<-chan time.Time, func())
	// PinBackoff overrides the pre-transaction pin retry backoff (tests).
	PinBackoff time.Duration
	// Instruments, when set, records the fence counters and the slack
	// histogram; nil uses the store's Instruments.
	Instruments *telemetry.Instruments
	// PinAttemptTimeout overrides the per-call pin bound when positive
	// (tests). Zero uses DefaultClaimFencePinAttemptTimeout capped at a third
	// of the claim lease.
	PinAttemptTimeout time.Duration
}

// ClaimPlaneFenced reports whether claimed commits through this store take
// the claim-plane fence: ClaimPlane is set and has a Pinner.
// collector.ClaimedService uses it to refuse a claim-plane control store
// paired with an in-tree committer, and the reverse (#9376 D8).
func (s IngestionStore) ClaimPlaneFenced() bool {
	return s.ClaimPlane != nil && s.ClaimPlane.Pinner != nil
}

// claimFenceSession is one plane-path commit's fence state, from the pin
// before BEGIN to the keeper stop after COMMIT or ROLLBACK.
type claimFenceSession struct {
	store     IngestionStore
	config    *ClaimPlaneFence
	request   fence.Request
	scopeID   string
	commitID  string
	keeper    *fence.Keeper
	stopTicks func()
	cancel    context.CancelCauseFunc
	parent    context.Context
	stopped   bool
	span      trace.Span
	recorded  bool
	slack     time.Duration
}

// beginClaimFence pins the claim before any core transaction opens and
// starts the re-pin keeper. The returned context is cancelled with the
// keeper's refusal when a re-pin comes back stale; the caller must run every
// transaction statement on it and must call finish exactly once.
func (s IngestionStore) beginClaimFence(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generationID string,
) (context.Context, *claimFenceSession, error) {
	config := s.ClaimPlane
	if config.Pinner == nil {
		return ctx, nil, errors.New("claim-plane commit fence: no claim pinner is configured")
	}
	if mutation.DerivedScannerImage {
		// The derived scanner heartbeat re-checks scanner attempt authority
		// on the fact transaction; the plane path has no core claim row to
		// do that on. Fail closed until the scanner path is ported.
		return ctx, nil, errors.New("claim-plane commit fence: derived scanner claims are not supported on the claim-plane path")
	}
	request, err := fence.RequestFor(mutation)
	if err != nil {
		return ctx, nil, err
	}
	margin := config.margin()
	if limit := request.Lease - request.Lease/3; margin >= limit {
		// D6: a healthy re-pin every lease/3 must leave the deadline above
		// the margin. A lease this short (or a margin this wide) would turn
		// every commit into a retryable deadline refusal, forever.
		return ctx, nil, fmt.Errorf(
			"claim-plane commit fence: margin %s must be below two thirds of the %s claim lease (%s)",
			margin, request.Lease, limit)
	}
	session := &claimFenceSession{store: s, config: config, request: request, scopeID: scopeValue.ScopeID}
	if config.Tracer != nil {
		ctx, session.span = config.Tracer.Start(ctx, telemetry.SpanIngestionCommitClaimFence, trace.WithAttributes(
			attribute.String("fence_mode", "plane"),
			attribute.String("claim_id", request.ClaimID),
			attribute.Int64("fencing_token", request.FencingToken),
			attribute.String("work_item_id", request.WorkItemID),
			attribute.String("generation_id", generationID),
		))
	}
	backoff := config.PinBackoff
	if backoff <= 0 {
		backoff = claimFencePinBackoff
	}
	firstDeadline := s.now().Add(request.Lease)
	attemptTimeout := config.pinAttemptTimeout(request.Lease)
	commitID, err := fence.Pin(ctx, config.Pinner, session.code(), request, firstDeadline,
		fence.Retry{Attempts: claimFencePinAttempts, Backoff: backoff, AttemptTimeout: attemptTimeout})
	if err != nil {
		session.recordRejection(ctx, err, generationID)
		session.endSpan(err)
		return ctx, nil, err
	}
	session.commitID = commitID
	if session.span != nil {
		session.span.SetAttributes(attribute.String("commit_id", commitID))
	}
	session.parent = ctx
	commitCtx, cancel := context.WithCancelCause(ctx)
	session.cancel = cancel
	ticks, stop := session.ticks()
	session.stopTicks = stop
	session.keeper = fence.StartKeeper(commitCtx, cancel, fence.KeeperConfig{
		Pinner: config.Pinner, Code: session.code(), Request: request,
		FirstDeadline: firstDeadline, Now: s.now, Ticks: ticks, AttemptTimeout: attemptTimeout,
		OnRepin: func(outcome string) { session.recordRepin(ctx, outcome) },
	})
	return commitCtx, session, nil
}

// margin is the configured deadline margin, DefaultClaimFenceMargin unless
// overridden.
func (c *ClaimPlaneFence) margin() time.Duration {
	if c.Margin > 0 {
		return c.Margin
	}
	return DefaultClaimFenceMargin
}

// pinAttemptTimeout is the bound on one pin call for a claim lease.
func (c *ClaimPlaneFence) pinAttemptTimeout(lease time.Duration) time.Duration {
	if c.PinAttemptTimeout > 0 {
		return c.PinAttemptTimeout
	}
	return min(DefaultClaimFencePinAttemptTimeout, lease/3)
}

// instruments is the fence's own Instruments, or the store's when unset.
func (f *claimFenceSession) instruments() *telemetry.Instruments {
	if f.config.Instruments != nil {
		return f.config.Instruments
	}
	return f.store.Instruments
}

func (f *claimFenceSession) code() fence.CodeFunc {
	if f.config.Code != nil {
		return f.config.Code
	}
	return workflow.ClaimPlaneErrorCode
}

func (f *claimFenceSession) ticks() (<-chan time.Time, func()) {
	interval := f.request.Lease / 3
	if f.config.Ticks != nil {
		return f.config.Ticks(interval)
	}
	ticker := time.NewTicker(interval)
	return ticker.C, ticker.Stop
}

// writeFence records the committed generation's fence row inside the fact
// transaction. generationID is the committed generation, which for
// terraform-state differs from the planned one the pin used.
func (f *claimFenceSession) writeFence(ctx context.Context, tx db.Queryer, generationID string) error {
	err := fence.Write(ctx, tx, fence.Row{
		GenerationID: generationID, WorkItemID: f.request.WorkItemID, ScopeID: f.scopeID,
		ClaimID: f.request.ClaimID, FencingToken: f.request.FencingToken,
		CommitID: f.commitID, CommittedAt: f.store.now(),
	})
	if err != nil {
		f.recordRejection(ctx, err, generationID)
	}
	return err
}

// checkDeadline is the last step before COMMIT. It records the slack sample
// and refuses when the latest pin deadline is within the margin.
func (f *claimFenceSession) checkDeadline(ctx context.Context, generationID string) error {
	if stale := f.keeper.Stale(); stale != nil {
		f.recordRejection(ctx, stale, generationID)
		return stale
	}
	slack, err := fence.CheckDeadline(f.keeper.LatestDeadline(), f.store.now(), f.config.margin())
	f.slack = slack
	if instruments := f.instruments(); instruments != nil && instruments.IngestionCommitPinSlack != nil {
		instruments.IngestionCommitPinSlack.Record(ctx, slack.Seconds())
	}
	if err != nil {
		f.recordRejection(ctx, err, generationID)
	}
	return err
}

// stopKeeper ends the re-pin loop, cancelling any re-pin in flight, and its
// ticker. It is idempotent; it runs on the commit goroutine only.
func (f *claimFenceSession) stopKeeper() {
	if f.stopped {
		return
	}
	f.stopped = true
	f.keeper.Stop()
	f.stopTicks()
}

// afterCommit stops the keeper as soon as COMMIT returns and gives back the
// context the commit started from. Post-commit work (the catalog merge and
// the relationship backfill) runs on it, so a re-pin refusal that lands after
// the generation is durable cannot cancel that work.
func (f *claimFenceSession) afterCommit() context.Context {
	f.stopKeeper()
	return f.parent
}

// finish stops the keeper and returns the commit's final error. A stale
// re-pin cancels the commit context, so the statement that failed reports a
// context error; finish replaces it with the keeper's terminal refusal so the
// collector fails the claim terminally instead of retrying.
func (f *claimFenceSession) finish(ctx context.Context, generationID string, err error) error {
	f.stopKeeper()
	if stale := f.keeper.Stale(); stale != nil && err != nil {
		var rejection *fence.Rejection
		if !errors.As(err, &rejection) {
			f.recordRejection(ctx, stale, generationID)
			err = errors.Join(stale, err)
		}
	}
	f.cancel(nil)
	f.endSpan(err)
	return err
}

func (f *claimFenceSession) endSpan(err error) {
	if f.span == nil {
		return
	}
	outcome := "committed"
	var rejection *fence.Rejection
	switch {
	case errors.As(err, &rejection):
		outcome = string(rejection.Reason)
		f.span.SetStatus(codes.Error, outcome)
	case err != nil:
		outcome = "error"
		f.span.SetStatus(codes.Error, err.Error())
	}
	f.span.SetAttributes(attribute.String("outcome", outcome))
	f.span.End()
}

// recordRejection counts and logs one fence refusal, once per commit.
func (f *claimFenceSession) recordRejection(ctx context.Context, err error, generationID string) {
	var rejection *fence.Rejection
	if f.recorded || !errors.As(err, &rejection) {
		return
	}
	f.recorded = true
	if instruments := f.instruments(); instruments != nil && instruments.IngestionClaimFenceRejections != nil {
		instruments.IngestionClaimFenceRejections.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionReason, string(rejection.Reason))))
	}
	if f.store.Logger != nil {
		// fence_claim_mismatch (equal token under another claim) is impossible
		// while the plane enforces UNIQUE (work_item_id, fencing_token): it is
		// an integrity alarm, not a routine refusal.
		level := slog.LevelWarn
		if rejection.Reason == fence.ReasonFenceClaimMismatch {
			level = slog.LevelError
		}
		f.store.Logger.Log(ctx, level, "claim-plane commit fence refused the commit",
			slog.String(telemetry.LogKeyScopeID, f.scopeID),
			slog.String(telemetry.LogKeyGenerationID, generationID),
			slog.String("work_item_id", f.request.WorkItemID),
			slog.String("claim_id", f.request.ClaimID),
			slog.String("reason", string(rejection.Reason)),
			slog.Bool("terminal", rejection.TerminalFailure()),
			slog.Int64("requested_fencing_token", rejection.RequestedToken),
			slog.Int64("stored_fencing_token", rejection.StoredToken),
			slog.Float64("pin_slack_seconds", f.slack.Seconds()),
			telemetry.PhaseAttr(telemetry.PhaseEmission),
		)
	}
}

func (f *claimFenceSession) recordRepin(ctx context.Context, outcome string) {
	if instruments := f.instruments(); instruments != nil && instruments.IngestionCommitRepins != nil {
		instruments.IngestionCommitRepins.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionOutcome, outcome)))
	}
}
