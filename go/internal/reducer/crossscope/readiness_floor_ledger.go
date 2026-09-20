// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"context"
	"log/slog"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// CheckProducerReadinessBeforeLoadWithLedger captures the same pre-load
// readiness signal as CheckProducerReadinessBeforeLoad, except the
// elapsed-time bound is NOT enforced here: it is enforced post-load against
// the (scope, domain) readiness-wait ledger, whose first-defer anchor
// survives supersession (#6814). The row-anchored early-out is bypassed by
// evaluating an unanchored copy of the intent; a nil ledger re-anchors at the
// row post-load via ReadWait synthesis, preserving the unwired bound exactly.
func CheckProducerReadinessBeforeLoadWithLedger(
	ctx context.Context,
	ledger ReadinessWaitLedger,
	readiness ProducerReadiness,
	intent reducercontract.Intent,
	now time.Time,
	crossScopeLookupPlanned bool,
) (ProducerReadinessSignal, error) {
	unanchored := intent
	unanchored.CycleStartedAt = time.Time{}
	unanchored.EnqueuedAt = time.Time{}
	return CheckProducerReadinessBeforeLoad(ctx, readiness, unanchored, now, crossScopeLookupPlanned)
}

// ApplyProducerReadinessPostLoad combines the pre-load signal with the
// post-load resolved evidence and the ledger, then does exactly one of:
// proceed (clearing a stood wait), defer (recording the wait and returning
// the non-counting not-ready error), or settle past the ledger-anchored bound
// (recording the settle and proceeding). Handlers replace their
// UnreadyProducers-plus-log-plus-error lines with this one call. A nil ledger
// keeps the pre-ledger per-row behavior: the bound falls back to the claimed
// row's own repair-cycle anchor.
func ApplyProducerReadinessPostLoad(
	ctx context.Context,
	logger *slog.Logger,
	ledger ReadinessWaitLedger,
	signal ProducerReadinessSignal,
	resolvedByProducer map[reducercontract.Domain]int,
	intent reducercontract.Intent,
	now time.Time,
) error {
	if signal.gateDisabled {
		return nil
	}
	unready := UnreadyProducers(signal, resolvedByProducer)
	existing, found, err := ReadWait(ctx, ledger, intent.ScopeID, intent.Domain, ReadinessCycleAnchor(intent))
	if err != nil {
		// A store that cannot answer is a real failure, classified like a
		// readiness-probe error: retryable and counting, so it surfaces
		// instead of deferring forever on a wait that was never recorded.
		return factload.ClassifyFactLoadError(err)
	}
	decision := DecideWait(WaitInput{
		Existing: existing, Found: found,
		ScopeID: intent.ScopeID, Domain: intent.Domain,
		GenerationID: intent.GenerationID, CycleStartedAt: intent.CycleStartedAt,
		Missing: producerDomainNames(unready), Now: now,
	})
	if len(unready) == 0 {
		return ApplyWaitDecision(ctx, ledger, decision, intent.ScopeID, intent.Domain)
	}
	if decision.Defer {
		logProducerReadinessDefer(ctx, logger, intent, unready, decision.Elapsed)
		if err := ApplyWaitDecision(ctx, ledger, decision, intent.ScopeID, intent.Domain); err != nil {
			return err
		}
		return NewProducerNotReadyError(intent.Domain, intent.ScopeID, intent.GenerationID, unready)
	}
	logProducerReadinessSettled(ctx, logger, intent, unready, decision.Elapsed)
	if err := ApplyWaitDecision(ctx, ledger, decision, intent.ScopeID, intent.Domain); err != nil {
		return err
	}
	return nil
}

// producerDomainNames renders the unready producer set as ledger missing keys.
func producerDomainNames(domains []reducercontract.Domain) []string {
	names := make([]string, 0, len(domains))
	for _, domain := range domains {
		names = append(names, string(domain))
	}
	return names
}

// logProducerReadinessDefer records a floor deferral. Message and attribute
// names match LogProducerNotReadyDefer so operator queries keep working; the
// elapsed is the ledger-anchored bound position (decision.Elapsed), which
// equals the row-anchored elapsed when no ledger is wired and the
// first-defer-anchored elapsed when one is, so a superseded row's fresh cycle
// start can never masquerade as a fresh wait. Outcome marks the ledger
// disposition for the same reason.
func logProducerReadinessDefer(
	ctx context.Context,
	logger *slog.Logger,
	intent reducercontract.Intent,
	unready []reducercontract.Domain,
	elapsed time.Duration,
) {
	if logger == nil {
		return
	}
	producers := make([]string, 0, len(unready))
	for _, producer := range unready {
		producers = append(producers, string(producer))
	}
	logger.LogAttrs(ctx, slog.LevelInfo,
		"cross-scope consumer deferred: producer scopes have not activated",
		log.Domain(string(intent.Domain)),
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.String("producer_domains", strings.Join(producers, ",")),
		slog.Duration("max_wait", ProducerReadinessMaxWait),
		slog.Duration("elapsed_since_cycle_start", elapsed),
		slog.String("outcome", ReadinessWaitDeferred),
	)
}

// logProducerReadinessSettled records the bounded outcome: the missing set
// outlived MaxWait since the first defer, so the consumer commits its
// best-available answer instead of deferring forever. Fires once per (scope,
// consumer, missing set); later generations with the same set settle at once.
func logProducerReadinessSettled(
	ctx context.Context,
	logger *slog.Logger,
	intent reducercontract.Intent,
	unready []reducercontract.Domain,
	elapsed time.Duration,
) {
	if logger == nil {
		return
	}
	producers := make([]string, 0, len(unready))
	for _, producer := range unready {
		producers = append(producers, string(producer))
	}
	logger.LogAttrs(ctx, slog.LevelInfo,
		"cross-scope consumer settled: producer wait bound reached, committing best-available answer",
		log.Domain(string(intent.Domain)),
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.String("producer_domains", strings.Join(producers, ",")),
		slog.Duration("max_wait", ProducerReadinessMaxWait),
		slog.Duration("elapsed_since_first_defer", elapsed),
		slog.String("outcome", ReadinessWaitAbandoned),
	)
}
