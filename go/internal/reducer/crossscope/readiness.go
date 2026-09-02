// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"fmt"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// ProducerNotReadyFailureClass is the durable failure_class a cross-scope
// consumer domain self-classifies with when a producer it declares a
// reducercontract.CrossScopeDependency on has not yet activated its
// generation for the relevant scope. Like the SecretsIAM and
// Kubernetes-correlation readiness classes, a retrying row in this class is
// deferred until its upstream producer commits, not failing on its own
// merits, so it is enrolled in nonCountingReducerRetryFailureClasses
// (go/internal/storage/postgres/reducer_queue_readiness_sql.go) to exempt
// retrying rows from the retry budget; that enrollment landed with its own
// attempt_count-freeze theory-proof
// (docs/internal/evidence/5709-attempt-count-freeze.md).
//
// Both registered cross-scope consumers produce this class:
// ci_cd_run_correlation and supply_chain_impact. Each samples
// ProducerReadiness before its cross-scope load and returns
// ProducerNotReadyError when the load resolved no producer output and the
// producer scopes have not activated (readiness_floor.go, and
// supply_chain_impact_evidence_load.go for the second consumer's
// producer-owned counting rule).
//
// Being in the dependency catalog is not what gates a consumer. Each handler
// opts in by calling the floor helpers and carrying the seam through its
// registration, so a third consumer added to the catalog produces this class
// only once its handler does the same.
const ProducerNotReadyFailureClass = "cross_scope_producer_not_ready"

// ProducerNotReadyError marks a cross-scope producer-readiness miss as
// retryable so the durable queue re-runs the consumer once the producer's
// generation activates, instead of writing an empty-join decision that never
// re-runs. It names only the consumer domain, the bounded producer domain set,
// and the scope/generation — never a specific uid, which could be a redacted
// identifier.
type ProducerNotReadyError struct {
	consumerDomain reducercontract.Domain
	scopeID        string
	generationID   string
	// ProducerDomains is exported so the reducer root -- which builds the
	// singleton batch-wide resolved-count map from this same bounded set --
	// can read it back without a package-private accessor. See the
	// Compatibility section of README.md.
	ProducerDomains []reducercontract.Domain
}

// NewProducerNotReadyError builds the readiness error for a consumer whose
// declared producers have not activated for this scope/generation.
func NewProducerNotReadyError(
	consumerDomain reducercontract.Domain,
	scopeID string,
	generationID string,
	producerDomains []reducercontract.Domain,
) ProducerNotReadyError {
	return ProducerNotReadyError{
		consumerDomain:  consumerDomain,
		scopeID:         scopeID,
		generationID:    generationID,
		ProducerDomains: producerDomains,
	}
}

func (e ProducerNotReadyError) Error() string {
	producers := make([]string, 0, len(e.ProducerDomains))
	for _, producer := range e.ProducerDomains {
		producers = append(producers, string(producer))
	}
	return fmt.Sprintf(
		"cross-scope producer(s) %s not active for consumer %s scope %s generation %s",
		strings.Join(producers, ","), e.consumerDomain, e.scopeID, e.generationID,
	)
}

// Retryable reports the readiness miss as retryable so the durable queue defers
// the consumer rather than dead-lettering it.
func (ProducerNotReadyError) Retryable() bool { return true }

// FailureClass returns the non-counting readiness class this error self-reports.
func (ProducerNotReadyError) FailureClass() string {
	return ProducerNotReadyFailureClass
}
