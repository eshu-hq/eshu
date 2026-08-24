// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azure

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildResourceMaterializationReducerIntent returns one reducer intent when
// azure_cloud_resource facts are present. The reducer materializes canonical
// CloudResource nodes, and the entity key is the readiness unit consumed by
// Azure relationship projection.
func BuildResourceMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.AzureCloudResourceFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainAzureResourceMaterialization,
		EntityKey:    "azure_resource_materialization:" + scopeID,
		Reason:       "azure runtime resource facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
