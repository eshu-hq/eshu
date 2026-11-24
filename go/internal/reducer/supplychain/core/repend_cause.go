// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The re-pend attribution signals (#10972): for each findings pass that
// commits its write, why the queue ran it and whether it changed anything.
// The next QA reading uses them to count the passes that re-ran for nothing.
// They observe only. No query, claim, ack, or write depends on them.

// passOutcomeLogMessage is the one INFO line a findings pass writes after its
// writer commit.
const passOutcomeLogMessage = "supply chain impact pass complete"

// rependCauseOf names why a findings pass was claimed, read only from the
// intent the claim already loaded. The order matters:
//
//  1. attempt count above one is a retry, whatever else the payload carries;
//  2. a fanout_remaining list is a continuation drain;
//  3. a fanout_origin stamp on a first attempt with no owed marker is a
//     fan-out rerun. That covers a reopen of a succeeded item and the replay
//     of an item that was in flight when the stamp landed: both leave the
//     same payload, so this function cannot tell them apart;
//  4. a cycle start later than the enqueue time means something re-pended the
//     item without a stamp (other_repend);
//  5. equal timestamps mean the item was never re-pended (first);
//  6. anything else is unknown, which includes an intent with no timestamps.
func rependCauseOf(intent reducercontract.Intent) string {
	switch {
	case intent.AttemptCount > 1:
		return telemetry.SupplyChainImpactRependCauseRetry
	case len(fanOutRemaining(intent)) > 0:
		return telemetry.SupplyChainImpactRependCauseDrain
	case fanOutOneHop(intent):
		return telemetry.SupplyChainImpactRependCauseFanOutRerun
	case intent.EnqueuedAt.IsZero() || intent.CycleStartedAt.IsZero():
		return telemetry.SupplyChainImpactRependCauseUnknown
	case intent.CycleStartedAt.After(intent.EnqueuedAt):
		return telemetry.SupplyChainImpactRependCauseOtherRepend
	default:
		return telemetry.SupplyChainImpactRependCauseFirst
	}
}

// passChangeOf classifies what a committed pass changed, from the write
// result the writer already returned. changed is the same condition the
// fan-out uses to set its owed marker: a changed or retracted CVE (suppression
// CVEs included) or a touched advisory group. change_class is none for an
// unchanged pass and unknown for a changed one: separating an enrichment-only
// change (EPSS and KEV derived fields) from another needs the stored row's
// fields, which the writer's digest read does not return, and reading them
// would add to the hot path.
func passChangeOf(write SupplyChainImpactWriteResult) (changed, class string) {
	if len(write.ChangedCVEIDs) > 0 || len(write.TouchedGroupIDs) > 0 {
		return telemetry.SupplyChainImpactPassChanged, telemetry.SupplyChainImpactChangeClassUnknown
	}
	return telemetry.SupplyChainImpactPassUnchanged, telemetry.SupplyChainImpactChangeClassNone
}

// emitPassOutcome records the pass-outcome counter point, the pass-complete
// log line, and the span attributes for one pass that committed its write.
// It runs right after the writer commit, before the advisory-group enqueue
// and the holder fan-out, so a pass whose fan-out later fails is still
// counted once: its retry counts again as a retry.
func (h SupplyChainImpactHandler) emitPassOutcome(
	ctx context.Context,
	intent reducercontract.Intent,
	loaded supplyChainImpactLoadedEvidence,
	write SupplyChainImpactWriteResult,
) {
	mode := supplyChainImpactPassMode(intent, loaded)
	cause := rependCauseOf(intent)
	changed, class := passChangeOf(write)
	if h.Instruments != nil && h.Instruments.SupplyChainImpactPassOutcomeTotal != nil {
		h.Instruments.SupplyChainImpactPassOutcomeTotal.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
			telemetry.AttrSupplyChainImpactPassMode(mode),
			telemetry.AttrSupplyChainImpactRependCause(cause),
			telemetry.AttrSupplyChainImpactPassChanged(changed),
			telemetry.AttrSupplyChainImpactChangeClass(class),
		))
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String(telemetry.MetricDimensionRependCause, cause),
		attribute.String(telemetry.MetricDimensionPassChanged, changed),
		attribute.String(telemetry.MetricDimensionChangeClass, class),
	)
	if h.Logger == nil {
		return
	}
	h.Logger.InfoContext(ctx, passOutcomeLogMessage,
		slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
		slog.String("scope_id", intent.ScopeID),
		slog.String("generation_id", intent.GenerationID),
		slog.String("intent_id", intent.IntentID),
		slog.Int("attempt_count", intent.AttemptCount),
		slog.String(telemetry.MetricDimensionPassMode, mode),
		slog.String(telemetry.MetricDimensionRependCause, cause),
		slog.String(telemetry.MetricDimensionPassChanged, changed),
		slog.String(telemetry.MetricDimensionChangeClass, class),
	)
}
