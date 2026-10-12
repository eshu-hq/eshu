// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Reprojector re-projects a scope's active generation without a snapshot and
// returns the refresh-skip reason it recorded. The ingestion store implements
// it (IngestionStore.ReprojectActiveGeneration); a resolver without it always
// parses.
type Reprojector interface {
	ReprojectActiveGeneration(ctx context.Context, request scope.ActiveReprojection) (string, error)
}

// Recorder carries the git collector's telemetry for Run. Both fields may be
// nil.
type Recorder struct {
	// Instruments carries eshu_dp_collector_reconcile_parse_skip_decisions_total.
	Instruments *telemetry.Instruments
	// Logger receives the decision logs.
	Logger *slog.Logger
}

// Attempt is one forced reconcile, read after the fetch.
type Attempt struct {
	// ScopeID is the scope.
	ScopeID string
	// Reason is the bounded reconcile reason.
	Reason string
	// State is the reconcile state the decision used; its Active* fields
	// describe the active generation.
	State scope.FullReconcileState
	// RemoteCommit is the fetched default-branch head.
	RemoteCommit string
	// Identity is the identity of a snapshot of RemoteCommit now.
	Identity string
	// HasSubmodules reports a .gitmodules file in the checkout.
	HasSubmodules bool
	// MaxSkips is the valve.
	MaxSkips int
	// ConfigDigest is ConfigDigest of the configuration, for the logs.
	ConfigDigest string
}

// Run decides one forced reconcile and, on a skip, asks reprojector to
// re-project the active generation. It reports true only when the store call
// succeeded; a store error is counted as read_error and the caller parses. It
// records one decision on the counter, a span event on ctx's span, an INFO log
// for a skip, and a WARN log for an identity mismatch or a store error.
func Run(ctx context.Context, attempt Attempt, clean func() (bool, error), reprojector Reprojector, recorder Recorder) bool {
	state := attempt.State
	input := Input{
		Reason: attempt.Reason,
		Active: ActiveGeneration{
			Present: state.ActiveGenerationID != "", GenerationID: state.ActiveGenerationID,
			CommitSHA: state.ActiveCommitSHA, Identity: state.ActivePreparseIdentity,
			IsDelta: state.ActiveIsDelta, Skips: state.ActivePreparseSkips,
		},
		RemoteCommit:  attempt.RemoteCommit,
		Identity:      attempt.Identity,
		HasSubmodules: attempt.HasSubmodules,
		MaxSkips:      attempt.MaxSkips,
	}
	outcome := Decide(input, clean)
	var storeErr error
	if outcome == OutcomeSkipped {
		if _, storeErr = reprojector.ReprojectActiveGeneration(ctx, scope.ActiveReprojection{
			ScopeID: attempt.ScopeID, GenerationID: state.ActiveGenerationID, CommitSHA: attempt.RemoteCommit,
			PreparseIdentity: attempt.Identity, Reason: attempt.Reason,
		}); storeErr != nil {
			outcome = OutcomeReadError
		}
	}
	recorder.record(ctx, attempt, input, outcome, storeErr)
	return outcome == OutcomeSkipped
}

func (r Recorder) record(ctx context.Context, attempt Attempt, input Input, outcome Outcome, storeErr error) {
	if r.Instruments != nil && r.Instruments.ReconcileParseSkipDecisions != nil {
		r.Instruments.ReconcileParseSkipDecisions.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionOutcome, string(outcome))))
	}
	trace.SpanFromContext(ctx).AddEvent("git.reconcile.parse_skip_decision", trace.WithAttributes(
		attribute.String("eshu.reconcile.parse_skip_outcome", string(outcome)),
		attribute.Bool("eshu.reconcile.parse_skipped", outcome == OutcomeSkipped)))
	if r.Logger == nil {
		return
	}
	attrs := []any{
		log.ScopeID(attempt.ScopeID), slog.String("reason", attempt.Reason), slog.String("outcome", string(outcome)),
		slog.String("generation_id", input.Active.GenerationID), slog.String("commit_sha", input.RemoteCommit),
		slog.Int("preparse_skips", input.Active.Skips), slog.String("config_digest", attempt.ConfigDigest),
	}
	switch {
	case outcome == OutcomeSkipped:
		r.Logger.InfoContext(ctx, "git_reconcile_parse_skipped", attrs...)
	case storeErr != nil:
		r.Logger.WarnContext(ctx, "git_reconcile_parse_skip_refused", append(attrs, slog.String("error", storeErr.Error()))...)
	case outcome == OutcomeIdentityMismatch:
		r.Logger.WarnContext(ctx, "git_reconcile_parse_skip_refused", attrs...)
	}
}
