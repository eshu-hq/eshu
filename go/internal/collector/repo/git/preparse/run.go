// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import (
	"context"
	"fmt"
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
	// StampWithoutParse is the opt-in first-sweep stamp (#8095). Off parses
	// as before.
	StampWithoutParse bool
	// StampProven reports the active generation proves it was produced by the
	// running content version and configuration with no degraded snapshot.
	// No producer records that proof yet, so it is false and the setting
	// parses as before; a follow-up records the proof on every parse.
	StampProven bool
	// ConfigDigest is ConfigDigest of the configuration, for the logs.
	ConfigDigest string
}

// Run decides one forced reconcile and, on a skip or a proven first-sweep
// stamp (#8095), asks reprojector to re-project the active generation. The
// store's answer sets the outcome (review F-3): reconcile_reprojected stays
// skipped (or stamped), reconcile_reprojection_pending
// becomes reprojection_pending, and neither parses, so Run reports true;
// reconcile_superseded becomes reprojection_superseded, and a store error or an
// unknown reason becomes read_error, so Run reports false and the caller parses.
// It records one decision on the counter, a span event on ctx's span, an INFO
// log when the parse is skipped, and a WARN log for an identity mismatch, a
// fenced re-open, or a store error.
func Run(ctx context.Context, attempt Attempt, clean func() (bool, error), reprojector Reprojector, recorder Recorder) bool {
	state := attempt.State
	input := Input{
		Reason: attempt.Reason,
		Active: ActiveGeneration{
			Present: state.ActiveGenerationID != "", GenerationID: state.ActiveGenerationID,
			CommitSHA: state.ActiveCommitSHA, Identity: state.ActivePreparseIdentity,
			IsDelta: state.ActiveIsDelta, Skips: state.ActivePreparseSkips,
		},
		RemoteCommit:      attempt.RemoteCommit,
		Identity:          attempt.Identity,
		HasSubmodules:     attempt.HasSubmodules,
		MaxSkips:          attempt.MaxSkips,
		StampWithoutParse: attempt.StampWithoutParse,
		StampProven:       attempt.StampProven,
	}
	outcome := Decide(input, clean)
	var storeErr error
	if outcome == OutcomeSkipped || outcome == OutcomeStamped {
		var reason string
		reason, storeErr = reprojector.ReprojectActiveGeneration(ctx, scope.ActiveReprojection{
			ScopeID: attempt.ScopeID, GenerationID: state.ActiveGenerationID, CommitSHA: attempt.RemoteCommit,
			PreparseIdentity: attempt.Identity, Reason: attempt.Reason,
		})
		outcome, storeErr = storeOutcome(outcome, reason, storeErr)
	}
	recorder.record(ctx, attempt, input, outcome, storeErr)
	return parseSkipped(outcome)
}

// storeOutcome maps the store's re-projection answer to the decision outcome.
// A re-opened stamp stays stamped so the counter tells stamps from skips.
// Any doubt parses: an error or a reason this code does not know is read_error.
func storeOutcome(decided Outcome, reason string, err error) (Outcome, error) {
	if err != nil {
		return OutcomeReadError, err
	}
	switch reason {
	case telemetry.RefreshSkipReasonReconcileReprojected:
		if decided == OutcomeStamped {
			return OutcomeStamped, nil
		}
		return OutcomeSkipped, nil
	case telemetry.RefreshSkipReasonReconcileReprojectionPending:
		return OutcomeReprojectionPending, nil
	case telemetry.RefreshSkipReasonReconcileSuperseded:
		return OutcomeReprojectionSuperseded, nil
	}
	return OutcomeReadError, fmt.Errorf("unknown re-projection reason %q", reason)
}

// parseSkipped reports whether the outcome leaves the repository unparsed.
func parseSkipped(outcome Outcome) bool {
	return outcome == OutcomeSkipped || outcome == OutcomeStamped || outcome == OutcomeReprojectionPending
}

func (r Recorder) record(ctx context.Context, attempt Attempt, input Input, outcome Outcome, storeErr error) {
	if r.Instruments != nil && r.Instruments.ReconcileParseSkipDecisions != nil {
		r.Instruments.ReconcileParseSkipDecisions.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionOutcome, string(outcome))))
	}
	trace.SpanFromContext(ctx).AddEvent("git.reconcile.parse_skip_decision", trace.WithAttributes(
		attribute.String(telemetry.LogKeyScopeID, attempt.ScopeID),
		attribute.String("eshu.reconcile.parse_skip_outcome", string(outcome)),
		attribute.Bool("eshu.reconcile.parse_skipped", parseSkipped(outcome))))
	if r.Logger == nil {
		return
	}
	attrs := []any{
		log.ScopeID(attempt.ScopeID), slog.String("reason", attempt.Reason), slog.String("outcome", string(outcome)),
		slog.String("generation_id", input.Active.GenerationID), slog.String("commit_sha", input.RemoteCommit),
		slog.Int("preparse_skips", input.Active.Skips), slog.String("config_digest", attempt.ConfigDigest),
	}
	switch {
	case parseSkipped(outcome):
		r.Logger.InfoContext(ctx, "git_reconcile_parse_skipped", attrs...)
	case storeErr != nil:
		r.Logger.WarnContext(ctx, "git_reconcile_parse_skip_refused", append(attrs, slog.String("error", storeErr.Error()))...)
	case outcome == OutcomeIdentityMismatch, outcome == OutcomeReprojectionSuperseded:
		r.Logger.WarnContext(ctx, "git_reconcile_parse_skip_refused", attrs...)
	}
}
