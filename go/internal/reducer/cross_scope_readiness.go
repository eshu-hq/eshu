// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"
)

// CrossScopeProducerNotReadyFailureClass is the durable failure_class a
// cross-scope consumer domain self-classifies with when a producer it declares a
// CrossScopeDependency on has not yet activated its generation for the relevant
// scope. Like the SecretsIAM and Kubernetes-correlation readiness classes, a
// retrying row in this class is deferred until its upstream producer commits,
// not failing on its own merits, so it is enrolled in
// nonCountingReducerRetryFailureClasses
// (go/internal/storage/postgres/reducer_queue_readiness_sql.go) to exempt
// retrying rows from the retry budget; that enrollment lands in this PR with its
// own attempt_count-freeze theory-proof
// (docs/internal/evidence/5709-attempt-count-freeze.md).
//
// The class is inert until a handler returns crossScopeProducerNotReadyError:
// the readiness-defer slice wires that. Until then nothing produces this class,
// so the enrollment changes no runtime behavior.
const CrossScopeProducerNotReadyFailureClass = "cross_scope_producer_not_ready"

// crossScopeProducerNotReadyError marks a cross-scope producer-readiness miss as
// retryable so the durable queue re-runs the consumer once the producer's
// generation activates, instead of writing an empty-join decision that never
// re-runs. It names only the consumer domain, the bounded producer domain set,
// and the scope/generation — never a specific uid, which could be a redacted
// identifier.
type crossScopeProducerNotReadyError struct {
	consumerDomain  Domain
	scopeID         string
	generationID    string
	producerDomains []Domain
}

// newCrossScopeProducerNotReadyError builds the readiness error for a consumer
// whose declared producers have not activated for this scope/generation.
func newCrossScopeProducerNotReadyError(
	consumerDomain Domain,
	scopeID string,
	generationID string,
	producerDomains []Domain,
) crossScopeProducerNotReadyError {
	return crossScopeProducerNotReadyError{
		consumerDomain:  consumerDomain,
		scopeID:         scopeID,
		generationID:    generationID,
		producerDomains: producerDomains,
	}
}

func (e crossScopeProducerNotReadyError) Error() string {
	producers := make([]string, 0, len(e.producerDomains))
	for _, producer := range e.producerDomains {
		producers = append(producers, string(producer))
	}
	return fmt.Sprintf(
		"cross-scope producer(s) %s not active for consumer %s scope %s generation %s",
		strings.Join(producers, ","), e.consumerDomain, e.scopeID, e.generationID,
	)
}

// Retryable reports the readiness miss as retryable so the durable queue defers
// the consumer rather than dead-lettering it.
func (crossScopeProducerNotReadyError) Retryable() bool { return true }

// FailureClass returns the non-counting readiness class this error self-reports.
func (crossScopeProducerNotReadyError) FailureClass() string {
	return CrossScopeProducerNotReadyFailureClass
}
