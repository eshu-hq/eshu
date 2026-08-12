// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "context"

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
//  3. readiness == nil — the seam is not wired for this handler. Unwired means
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
	consumer Domain,
	scopeID string,
	generationID string,
	resolved int,
) error {
	if resolved > 0 {
		return nil
	}
	dependencies := crossScopeDependenciesForRegistration(consumer)
	if len(dependencies) == 0 {
		return nil
	}
	if readiness == nil {
		return nil
	}
	ready, err := readiness.CrossScopeProducersReady(ctx, consumer, scopeID, generationID)
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
		consumer,
		scopeID,
		generationID,
		dependencies[0].ProducerDomains,
	)
}
