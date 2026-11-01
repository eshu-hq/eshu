// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The fan-out's operator signals: its span, its counter outcomes, its log
// lines, and the result sub-signals.

func (h SupplyChainImpactHandler) startFanOutSpan(ctx context.Context, intent reducercontract.Intent) (context.Context, trace.Span) {
	if h.Tracer == nil {
		// A no-op span: ending it must never end the caller's span.
		return ctx, trace.SpanFromContext(context.Background())
	}
	return h.Tracer.Start(ctx, telemetry.SpanSupplyChainImpactFanOut, trace.WithAttributes(
		attribute.String("scope_id", intent.ScopeID),
		attribute.String("generation_id", intent.GenerationID),
	))
}

func (h SupplyChainImpactHandler) countFanOut(ctx context.Context, outcome string, n int) {
	if n <= 0 || h.Instruments == nil || h.Instruments.SupplyChainImpactFanOut == nil {
		return
	}
	h.Instruments.SupplyChainImpactFanOut.Add(ctx, int64(n), metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		attribute.String("outcome", outcome),
	))
}

// emitFanOut records the pass's counters and its one summary log line. The
// per-holder detail is a debug line (logFanOutHolder) so a 256-holder claim
// does not write 256 info lines.
func (h SupplyChainImpactHandler) emitFanOut(ctx context.Context, intent reducercontract.Intent, r fanOutResult) {
	h.countFanOut(ctx, string(FanOutReopened), r.reopened)
	h.countFanOut(ctx, string(FanOutReplayFlagged), r.replayFlagged)
	h.countFanOut(ctx, string(FanOutPendingNoop), r.pendingNoop)
	h.countFanOut(ctx, string(FanOutEnqueued), r.enqueued)
	h.countFanOut(ctx, fanOutOutcomeDeferred, r.newlyDeferred)
	if r.crashRecovery {
		h.countFanOut(ctx, fanOutOutcomeCrashRecovery, 1)
	}
	if h.Logger == nil {
		return
	}
	h.Logger.InfoContext(ctx, "supply chain impact holder fan-out",
		slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
		slog.String("scope_id", intent.ScopeID),
		slog.String("generation_id", intent.GenerationID),
		slog.String("intent_id", intent.IntentID),
		slog.Int("holders", r.holders),
		slog.Int("reopened", r.reopened),
		slog.Int("replay_flagged", r.replayFlagged),
		slog.Int("pending_noop", r.pendingNoop),
		slog.Int("enqueued", r.enqueued),
		slog.Int("deferred", r.deferred),
		slog.Bool("crash_recovery", r.crashRecovery),
	)
}

func (h SupplyChainImpactHandler) logFanOutHolder(ctx context.Context, intent reducercontract.Intent, holder string, outcome FanOutOutcome) {
	if h.Logger == nil {
		return
	}
	h.Logger.DebugContext(ctx, "supply chain impact holder reopened",
		slog.String("source_scope_id", intent.ScopeID),
		slog.String("source_generation_id", intent.GenerationID),
		slog.String("holder_scope_id", holder),
		slog.String("outcome", string(outcome)),
	)
}

// readFailureOutcome maps a holder read error to its counter outcome.
func readFailureOutcome(err error) string {
	if errors.Is(err, ErrFanOutReadTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return fanOutOutcomeReadTimeout
	}
	return fanOutOutcomeReadError
}

// mergeSubSignals adds the fan-out counts to a result's sub-signals.
func (r fanOutResult) mergeSubSignals(subSignals map[string]float64) {
	subSignals["fanout_holders"] = float64(r.holders)
	subSignals["fanout_reopened"] = float64(r.reopened)
	subSignals["fanout_replay_flagged"] = float64(r.replayFlagged)
	subSignals["fanout_deferred"] = float64(r.deferred)
}
