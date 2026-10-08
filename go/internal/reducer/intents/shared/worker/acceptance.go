// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// AcceptanceLookupEvent is the root spelling of one accepted-generation
// lookup outcome (moved here from the reducer root's
// sharedAcceptanceLookupEvent, issue #6061).
type AcceptanceLookupEvent struct {
	Runner   string
	Result   string
	Duration float64
	Err      error
}

// AcceptanceTelemetry centralizes reducer acceptance metrics and logs
// so the Option B storage implementation can plug into one stable
// observability contract (moved here from the reducer root's
// sharedAcceptanceTelemetry, issue #6061). The code-call projection runner
// and selection files (still at the reducer root) also use it.
type AcceptanceTelemetry struct {
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

func (t AcceptanceTelemetry) RecordLookup(ctx context.Context, event AcceptanceLookupEvent) {
	if t.Instruments != nil {
		t.Instruments.SharedAcceptanceLookupDuration.Record(
			ctx,
			event.Duration,
			metric.WithAttributes(
				telemetry.AttrRunner(event.Runner),
				telemetry.AttrLookupResult(event.Result),
			),
		)
		if event.Err != nil {
			t.Instruments.SharedAcceptanceLookupErrors.Add(
				ctx,
				1,
				metric.WithAttributes(
					telemetry.AttrRunner(event.Runner),
					telemetry.AttrErrorType("lookup_failed"),
				),
			)
		}
	}

	if t.Logger == nil || event.Err == nil {
		return
	}

	t.Logger.ErrorContext(
		ctx,
		"shared acceptance lookup failed",
		slog.String("runner", event.Runner),
		slog.String("lookup_result", event.Result),
		slog.String("error_type", "lookup_failed"),
		log.Err(event.Err),
		slog.Float64("duration_seconds", event.Duration),
		telemetry.FailureClassAttr("shared_acceptance_lookup_error"),
		telemetry.PhaseAttr(telemetry.PhaseShared),
	)
}

// Stale-intent reasons recorded on eshu_dp_shared_projection_stale_intents_total
// as the closed "reason" attribute. acceptance_mismatch is the intent's
// generation differing from the accepted generation for its acceptance key;
// generation_superseded is a readiness-blocked intent whose scope generation
// is terminally superseded (#7121): it was superseded before workload
// materialization ran, so no phase row will ever unblock it;
// covered_by_full_successor is an intent whose superseded generation is
// covered by a newer emitted full generation (#7165): the successor re-emits
// its edges, so replaying it would be a wasted retract/write cycle.
const (
	StaleReasonAcceptanceMismatch     = "acceptance_mismatch"
	StaleReasonGenerationSuperseded   = "generation_superseded"
	StaleReasonCoveredByFullSuccessor = "covered_by_full_successor"
)

// RecordStaleIntents records stale intents drained because the accepted
// generation for their acceptance key differs from the intent's generation.
func (t AcceptanceTelemetry) RecordStaleIntents(ctx context.Context, runner string, domain string, staleCount int) {
	t.recordStale(ctx, runner, domain, StaleReasonAcceptanceMismatch, "shared acceptance filtered stale intents", staleCount)
}

// RecordSupersededGenerationIntents records intents drained because their scope
// generation is superseded (#7121). The reason attribute keeps them apart from
// acceptance mismatches so operators can tell orphan cleanup from churn.
func (t AcceptanceTelemetry) RecordSupersededGenerationIntents(ctx context.Context, runner string, domain string, count int) {
	t.recordStale(ctx, runner, domain, StaleReasonGenerationSuperseded, "shared projection drained intents of superseded generations", count)
}

// RecordCoveredByFullSuccessorIntents records intents drained because their
// superseded generation is covered by a newer emitted full generation
// (#7165). The reason attribute keeps them apart from acceptance mismatches
// and from the #7121 orphan drain, so operators can tell skipped replays from
// churn and from orphan cleanup.
func (t AcceptanceTelemetry) RecordCoveredByFullSuccessorIntents(ctx context.Context, runner string, domain string, count int) {
	t.recordStale(ctx, runner, domain, StaleReasonCoveredByFullSuccessor, "shared projection drained intents covered by an emitted full successor", count)
}

func (t AcceptanceTelemetry) recordStale(
	ctx context.Context,
	runner string,
	domain string,
	reason string,
	message string,
	staleCount int,
) {
	if staleCount <= 0 {
		return
	}

	if t.Instruments != nil {
		t.Instruments.SharedProjectionStaleIntents.Add(
			ctx,
			int64(staleCount),
			metric.WithAttributes(
				telemetry.AttrDomain(domain),
				telemetry.AttrRunner(runner),
				telemetry.AttrReason(reason),
			),
		)
	}

	if t.Logger == nil {
		return
	}

	t.Logger.InfoContext(
		ctx,
		message,
		slog.String("runner", runner),
		log.Domain(domain),
		slog.String("stale_reason", reason),
		telemetry.AcceptanceStaleCountAttr(staleCount),
		telemetry.PhaseAttr(telemetry.PhaseShared),
	)
}
