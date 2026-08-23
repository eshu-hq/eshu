// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azure

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildRelationshipMaterializationReducerIntent returns one reducer intent when
// azure_cloud_relationship facts are present. The reducer projects canonical
// Azure relationship graph edges. This builder shares the Azure resource entity
// key so the edge handler gates on the same canonical-nodes publication.
func BuildRelationshipMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.AzureCloudRelationshipFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainAzureRelationshipMaterialization,
		EntityKey:    "azure_resource_materialization:" + scopeID,
		Reason:       "azure runtime relationship facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
