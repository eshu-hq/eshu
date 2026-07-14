// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildObservabilityCoverageCorrelationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstMatchingKindPredicate(
		observabilityCoverageCorrelationCandidateFactKind,
		observabilityCoverageCorrelationTriggerFact,
	)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainObservabilityCoverageCorrelation,
		EntityKey:    "observability_coverage_correlation:" + scopeValue.ScopeID,
		Reason:       observabilityCoverageCorrelationReason(envelope),
		FactID:       envelope.FactID,
		SourceSystem: observabilitySourceSystem(envelope),
	}, true
}

// observabilityCoverageCorrelationCandidateFactKind reports whether kind can
// EVER satisfy observabilityCoverageCorrelationTriggerFact. It mirrors that
// function's kind-level branches so firstMatchingKindPredicate only visits
// facts of kinds that have a chance of matching; the AWS branch still needs
// its per-envelope resource_type decode, which stays in
// observabilityCoverageCorrelationTriggerFact as the final accept check.
func observabilityCoverageCorrelationCandidateFactKind(kind string) bool {
	if kind == facts.AWSResourceFactKind {
		return true
	}
	if kind == facts.ObservabilitySourceInstanceFactKind {
		return false
	}
	_, ok := facts.ObservabilitySchemaVersion(kind)
	return ok
}

func observabilityCoverageCorrelationTriggerFact(envelope facts.Envelope) bool {
	if envelope.FactKind == facts.AWSResourceFactKind {
		_, ok := observabilityResourceTypes[awsResourceTypeForEnvelope(envelope)]
		return ok
	}
	if envelope.FactKind == facts.ObservabilitySourceInstanceFactKind {
		return false
	}
	_, ok := facts.ObservabilitySchemaVersion(envelope.FactKind)
	return ok
}

func observabilityCoverageCorrelationReason(envelope facts.Envelope) string {
	if envelope.FactKind == facts.AWSResourceFactKind {
		return "aws observability resource facts observed"
	}
	return "observability source facts observed"
}

func observabilitySourceSystem(envelope facts.Envelope) string {
	if envelope.SourceRef.SourceSystem != "" {
		return envelope.SourceRef.SourceSystem
	}
	if envelope.CollectorKind != "" {
		return envelope.CollectorKind
	}
	return "observability"
}
