// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildPackageSourceCorrelationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.PackageRegistrySourceHintFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainPackageSourceCorrelation,
			EntityKey:    "package_source_correlation:" + scopeValue.ScopeID,
			Reason:       "package registry source hints observed",
			FactID:       envelope.FactID,
			SourceSystem: packageSourceCorrelationSourceSystem(envelope),
		}, true
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.PackageRegistryPackageFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainPackageSourceCorrelation,
			EntityKey:    "package_source_correlation:" + scopeValue.ScopeID,
			Reason:       "package registry identity observed",
			FactID:       envelope.FactID,
			SourceSystem: packageSourceCorrelationSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}

func packageSourceCorrelationSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
