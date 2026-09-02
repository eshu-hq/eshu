// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// This file is the transitional compatibility surface for the cross-scope
// producer-readiness floor and dependency catalog that moved to [crossscope]
// (issue #6061). Reducer-root call sites keep their current spelling; each
// entry is deleted once its last caller has moved into a family subpackage.

// CrossScopeDependency declares that a consumer reducer domain reads canonical
// facts a producer domain writes in a DIFFERENT ingestion scope. The consumer's
// cross-scope active-fact load can run before the producer has committed its
// latest output, so producer completion must schedule the canonical consumer
// again.
type CrossScopeDependency = reducercontract.CrossScopeDependency

// CrossScopeConsumerDomains forwards to [crossscope.ConsumerDomains].
func CrossScopeConsumerDomains() []Domain {
	return crossscope.ConsumerDomains()
}

// CrossScopeCompletionEdge is one producer-to-consumer fanout edge derived
// from the cross-scope dependency catalog.
type CrossScopeCompletionEdge = crossscope.CompletionEdge

// CrossScopeCompletionEdges forwards to [crossscope.CompletionEdges].
func CrossScopeCompletionEdges() []CrossScopeCompletionEdge {
	return crossscope.CompletionEdges()
}

// crossScopeDependenciesForRegistration forwards to
// [crossscope.DependenciesForRegistration].
func crossScopeDependenciesForRegistration(domain Domain) []CrossScopeDependency {
	return crossscope.DependenciesForRegistration(domain)
}

// CrossScopeProducerNotReadyFailureClass is the durable failure_class a
// cross-scope consumer domain self-classifies with when a producer it declares
// a CrossScopeDependency on has not yet activated its generation for the
// relevant scope. See [crossscope.ProducerNotReadyFailureClass].
const CrossScopeProducerNotReadyFailureClass = crossscope.ProducerNotReadyFailureClass

// crossScopeProducerNotReadyError marks a cross-scope producer-readiness miss
// as retryable. See [crossscope.ProducerNotReadyError].
type crossScopeProducerNotReadyError = crossscope.ProducerNotReadyError

// newCrossScopeProducerNotReadyError forwards to
// [crossscope.NewProducerNotReadyError].
func newCrossScopeProducerNotReadyError(
	consumerDomain Domain,
	scopeID string,
	generationID string,
	producerDomains []Domain,
) crossScopeProducerNotReadyError {
	return crossscope.NewProducerNotReadyError(consumerDomain, scopeID, generationID, producerDomains)
}

// CrossScopeProducerReadiness answers whether the producer scopes a consumer
// depends on have finished publishing. See [crossscope.ProducerReadiness].
type CrossScopeProducerReadiness = crossscope.ProducerReadiness

// CrossScopeProducerReadinessByDomain answers readiness for each producer
// domain separately. See [crossscope.ProducerReadinessByDomain].
type CrossScopeProducerReadinessByDomain = crossscope.ProducerReadinessByDomain

// crossScopeProducerReadinessSignal is the floor's answer, captured BEFORE the
// consumer's cross-scope load runs. See [crossscope.ProducerReadinessSignal].
type crossScopeProducerReadinessSignal = crossscope.ProducerReadinessSignal

// checkCrossScopeProducerReadinessBeforeLoad forwards to
// [crossscope.CheckProducerReadinessBeforeLoad].
func checkCrossScopeProducerReadinessBeforeLoad(
	ctx context.Context,
	readiness CrossScopeProducerReadiness,
	intent Intent,
	now time.Time,
	crossScopeLookupPlanned bool,
) (crossScopeProducerReadinessSignal, error) {
	return crossscope.CheckProducerReadinessBeforeLoad(ctx, readiness, intent, now, crossScopeLookupPlanned)
}

// crossScopeUnreadyProducers forwards to [crossscope.UnreadyProducers].
func crossScopeUnreadyProducers(
	signal crossScopeProducerReadinessSignal,
	resolvedByProducer map[Domain]int,
) []Domain {
	return crossscope.UnreadyProducers(signal, resolvedByProducer)
}

// singleProducerResolvedCounts forwards to
// [crossscope.SingleProducerResolvedCounts].
func singleProducerResolvedCounts(producers []Domain, resolved int) map[Domain]int {
	return crossscope.SingleProducerResolvedCounts(producers, resolved)
}

// logCrossScopeProducerNotReadyDefer forwards to
// [crossscope.LogProducerNotReadyDefer].
func logCrossScopeProducerNotReadyDefer(
	ctx context.Context,
	logger *slog.Logger,
	intent Intent,
	now time.Time,
	producerDomains []Domain,
) {
	crossscope.LogProducerNotReadyDefer(ctx, logger, intent, now, producerDomains)
}
