// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagesource

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildPackageSourceCorrelationReducerIntent enqueues one reducer intent that
// asks the reducer to classify the scope generation's package-registry source
// hints and manifest-backed package consumption against active Git facts. It
// anchors to the first package_registry.source_hint fact in original input
// order; when the generation carries no source hint it falls back to the first
// package_registry.package identity fact, so a registry generation that has
// package identity but no repository hints still enqueues the classifier once.
// Kind priority, not input position, picks the anchor: a source hint placed
// after an identity fact still wins. Only envelope.FactKind is read — the
// payload is never decoded here, so a malformed hint never fails the build. A
// generation with neither kind enqueues nothing.
func BuildPackageSourceCorrelationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	if envelope, ok := lookup.FirstOfKind(facts.PackageRegistrySourceHintFactKind); ok {
		return projectorintent.ReducerIntent{
			ScopeID:      scopeID,
			GenerationID: generationID,
			Domain:       reducer.DomainPackageSourceCorrelation,
			EntityKey:    "package_source_correlation:" + scopeID,
			Reason:       "package registry source hints observed",
			FactID:       envelope.FactID,
			SourceSystem: projectorintent.SourceSystem(envelope),
		}, true
	}
	if envelope, ok := lookup.FirstOfKind(facts.PackageRegistryPackageFactKind); ok {
		return projectorintent.ReducerIntent{
			ScopeID:      scopeID,
			GenerationID: generationID,
			Domain:       reducer.DomainPackageSourceCorrelation,
			EntityKey:    "package_source_correlation:" + scopeID,
			Reason:       "package registry identity observed",
			FactID:       envelope.FactID,
			SourceSystem: projectorintent.SourceSystem(envelope),
		}, true
	}
	return projectorintent.ReducerIntent{}, false
}
