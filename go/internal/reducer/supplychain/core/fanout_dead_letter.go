// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The per-holder reopen outcomes for a holder whose item cannot be reopened
// the ordinary way (#11147). A dead-lettered holder named by a change keeps a
// stale copy the documented bound does not cover, so the fan-out reopens it as
// a full genuine pass unless its failure class is in the admin unsafe-replay
// set.
const (
	// FanOutDeadLetterReopened means a dead letter at the holder's active
	// generation went back to pending as a full genuine pass: attempts zeroed,
	// no origin stamp, owed and remaining holders kept.
	FanOutDeadLetterReopened FanOutOutcome = "dead_letter_reopened"
	// FanOutDeadLetterSkipped means the active generation's item is a dead
	// letter in an unsafe-replay class, so it stays dead and its copies stay
	// pinned until an operator addresses the cause and replays it.
	FanOutDeadLetterSkipped FanOutOutcome = "dead_letter_skipped"
	// FanOutNoActiveGeneration means the holder scope has no active
	// generation, so there is nothing to reopen or enqueue.
	FanOutNoActiveGeneration FanOutOutcome = "no_active_generation"
)

// FanOutReopen is what reopening one holder did. PriorFailureClass is the
// dead letter's failure class for FanOutDeadLetterReopened and
// FanOutDeadLetterSkipped, and empty for every other outcome.
type FanOutReopen struct {
	Outcome           FanOutOutcome
	PriorFailureClass string
}

// recordHolderReopen adds one holder's reopen to the pass result and writes
// its operator signals. A dead-letter outcome is counted here, per holder,
// because its counter series carries the prior failure class, and it writes
// one info line, because a reopened or skipped dead letter is rare and an
// operator needs each one. Every other outcome is a debug line and is counted
// with the pass in emitFanOut, so a 256-holder claim does not write 256 info
// lines.
func (h SupplyChainImpactHandler) recordHolderReopen(
	ctx context.Context,
	intent reducercontract.Intent,
	result *fanOutResult,
	holder string,
	reopen FanOutReopen,
) {
	result.record(reopen.Outcome)
	switch reopen.Outcome {
	case FanOutDeadLetterReopened, FanOutDeadLetterSkipped:
		h.countFanOutDeadLetter(ctx, result.cause, reopen)
		h.logFanOutDeadLetter(ctx, intent, holder, reopen)
	default:
		h.logFanOutHolder(ctx, intent, holder, reopen.Outcome)
	}
}

func (h SupplyChainImpactHandler) countFanOutDeadLetter(ctx context.Context, cause string, reopen FanOutReopen) {
	if h.Instruments == nil || h.Instruments.SupplyChainImpactFanOut == nil {
		return
	}
	if cause == "" {
		cause = fanOutCauseOther
	}
	h.Instruments.SupplyChainImpactFanOut.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		attribute.String("outcome", string(reopen.Outcome)),
		attribute.String("cause", cause),
		telemetry.AttrFailureClass(reopen.PriorFailureClass),
	))
}

func (h SupplyChainImpactHandler) logFanOutDeadLetter(ctx context.Context, intent reducercontract.Intent, holder string, reopen FanOutReopen) {
	if h.Logger == nil {
		return
	}
	message := "supply chain impact dead-lettered holder reopened"
	if reopen.Outcome == FanOutDeadLetterSkipped {
		message = "supply chain impact dead-lettered holder left dead: unsafe-replay failure class"
	}
	h.Logger.InfoContext(ctx, message,
		slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
		slog.String("source_scope_id", intent.ScopeID),
		slog.String("source_generation_id", intent.GenerationID),
		slog.String("holder_scope_id", holder),
		slog.String("outcome", string(reopen.Outcome)),
		slog.String("prior_failure_class", reopen.PriorFailureClass),
	)
}
