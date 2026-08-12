// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"
)

// crossScopeProducerReadinessMaxWait bounds the deferral by ELAPSED TIME since
// the current repair cycle began, so a consumer converges instead of waiting
// forever on a producer that never arrives.
//
// The bound is not optional and it MUST NOT be a retry-count comparison.
// CrossScopeProducerNotReadyFailureClass is enrolled in
// nonCountingReducerRetryFailureClasses (reducer_queue_readiness_sql.go), which
// FREEZES fact_work_items.attempt_count for exactly this class -- by design, so
// a waiting consumer is never dead-lettered. That freeze means Intent.AttemptCount
// stops advancing after the first defer and reads identically on every later
// claim, so a bound compared against it can never fire. The sibling AWS gate
// shipped that mistake first and had to be re-proven against the real queue.
//
// Without a bound, a producer scope that is permanently absent, permanently
// failed, or stuck leaves its consumers deferring forever in a class that never
// counts and never dead-letters. That is a worse failure than the durable empty
// answer this floor exists to prevent: the empty answer is at least visible and
// repairable, where an eternal defer is silent.
//
// 30 minutes matches the sibling gate rather than a separate measurement:
// generous enough that ordinary asynchronous producer ingestion never reaches
// it, short enough that a genuinely stuck producer converges to a terminal
// answer inside an operator's normal triage window.
const crossScopeProducerReadinessMaxWait = 30 * time.Minute

// CrossScopeProducerReadiness answers whether every producer domain a consumer
// declares in crossScopeDependencyCatalog has committed output that the
// consumer's scope can already see.
//
// This is the correctness floor for the cross-scope dependency contract
// (#5709). The producer-completion fanout re-runs a consumer AFTER a producer
// acknowledges, which is the right re-trigger — but it cannot help a consumer
// that was already claimed when the producer finished. That consumer resolves
// nothing, writes a durable "no answer" decision, and no later event disturbs
// it. The floor is what makes such a consumer defer instead.
//
// Deliberately an interface consumed here rather than a concrete store: the
// reducer package already takes its readiness seams this way (see
// GraphProjectionReadinessLookup), and it keeps this package free of SQL.
type CrossScopeProducerReadiness interface {
	// CrossScopeProducersReady reports whether the declared producers for
	// consumer have committed output visible to scopeID/generationID.
	//
	// Returning (false, nil) means "not yet" and is the deferrable case.
	// Returning an error means the readiness question itself could not be
	// answered, which is NOT a readiness miss and must not be reported as one.
	CrossScopeProducersReady(
		ctx context.Context,
		consumer Domain,
		scopeID string,
		generationID string,
	) (bool, error)
}

// deferWhenCrossScopeProducersNotReady returns the non-counting readiness error
// when a consumer's cross-scope load resolved nothing AND a declared producer
// has not yet committed for this scope. It returns nil in every other case, so
// callers can wrap an existing load site without changing its success path.
//
// The three conditions are all load-bearing, and the order matters:
//
//  1. resolved > 0 — the consumer HAS evidence, so producer readiness is moot.
//     Checking readiness first would defer a consumer that already has its
//     answer, turning a working pass into a retry loop.
//  2. no declared producers — the domain is not a registered cross-scope
//     consumer, so there is nothing to wait for. A domain absent from the
//     catalog must behave exactly as it does today.
//  3. the elapsed bound is reached — converge on the best available answer
//     rather than deferring forever (see crossScopeProducerReadinessMaxWait).
//  4. readiness == nil — the seam is not wired for this handler. Unwired means
//     "no floor", not "not ready": defaulting to defer would strand every
//     consumer in a deployment that has not adopted the seam.
//
// An error from the readiness lookup is returned as-is rather than converted
// into a readiness miss. A store that cannot answer is a real failure and
// should be classified on its own merits; reporting it as
// cross_scope_producer_not_ready would hide it in a class that never counts
// against the retry budget, so it could retry forever without ever surfacing.
func deferWhenCrossScopeProducersNotReady(
	ctx context.Context,
	readiness CrossScopeProducerReadiness,
	intent Intent,
	now time.Time,
	resolved int,
) error {
	if resolved > 0 {
		return nil
	}
	dependencies := crossScopeDependenciesForRegistration(intent.Domain)
	if len(dependencies) == 0 {
		return nil
	}
	if readiness == nil {
		return nil
	}
	if anchor := crossScopeReadinessCycleAnchor(intent); !anchor.IsZero() &&
		now.Sub(anchor) >= crossScopeProducerReadinessMaxWait {
		return nil
	}
	ready, err := readiness.CrossScopeProducersReady(ctx, intent.Domain, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return err
	}
	if ready {
		// Producers are quiescent and the join is still empty, so the empty
		// answer is the true one. Converge on a durable unresolved decision
		// rather than deferring forever.
		return nil
	}
	return newCrossScopeProducerNotReadyError(
		intent.Domain,
		intent.ScopeID,
		intent.GenerationID,
		dependencies[0].ProducerDomains,
	)
}

// crossScopeReadinessCycleAnchor returns intent.CycleStartedAt when set,
// falling back to intent.EnqueuedAt.
//
// CycleStartedAt is COALESCE(reopened_at, created_at) from the claim query, so
// it is the only one of the two that gets a fresh value when a maintenance
// pass reopens the row. Anchoring on EnqueuedAt alone would read as "already
// past the bound" on the first claim of any reopened row, skipping the
// readiness lookup entirely and committing a possibly-early answer with no
// grace window -- the regression a round-3 review caught on the sibling AWS
// gate (see awsCloudRuntimeDriftStatePendingMaxWait).
//
// A zero anchor means elapsed time is unknown, not infinite. Subtracting a
// zero time.Time from now reads as tens of thousands of hours and would fire
// the terminal fallback on the very first defer. Returning zero here keeps the
// caller deferring, which costs a bounded delay; the other direction commits a
// possibly-wrong answer for a reason unrelated to elapsed time.
func crossScopeReadinessCycleAnchor(intent Intent) time.Time {
	if !intent.CycleStartedAt.IsZero() {
		return intent.CycleStartedAt
	}
	return intent.EnqueuedAt
}
