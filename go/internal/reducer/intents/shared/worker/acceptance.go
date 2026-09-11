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

// SharedAcceptanceLookupEvent is the root spelling of one accepted-generation
// lookup outcome (moved here from the reducer root's
// sharedAcceptanceLookupEvent, issue #6061).
type SharedAcceptanceLookupEvent struct {
	Runner   string
	Result   string
	Duration float64
	Err      error
}

// SharedAcceptanceTelemetry centralizes reducer acceptance metrics and logs
// so the Option B storage implementation can plug into one stable
// observability contract (moved here from the reducer root's
// sharedAcceptanceTelemetry, issue #6061). The code-call projection runner
// and selection files (still at the reducer root) also use it.
type SharedAcceptanceTelemetry struct {
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

func (t SharedAcceptanceTelemetry) RecordLookup(ctx context.Context, event SharedAcceptanceLookupEvent) {
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

func (t SharedAcceptanceTelemetry) RecordStaleIntents(ctx context.Context, runner string, domain string, staleCount int) {
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
			),
		)
	}

	if t.Logger == nil {
		return
	}

	t.Logger.InfoContext(
		ctx,
		"shared acceptance filtered stale intents",
		slog.String("runner", runner),
		log.Domain(domain),
		telemetry.AcceptanceStaleCountAttr(staleCount),
		telemetry.PhaseAttr(telemetry.PhaseShared),
	)
}
