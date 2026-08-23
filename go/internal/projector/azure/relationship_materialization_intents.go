// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azure

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildAzureRelationshipMaterializationReducerIntent enqueues one reducer intent
// that projects azure_cloud_relationship facts into canonical Azure relationship
// graph edges. It shares the Azure resource materialization entity key so the
// edge handler gates on the same canonical-nodes readiness publication.
func BuildRelationshipMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	lookup projectorintent.FactLookup,
) (ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.AzureCloudRelationshipFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainAzureRelationshipMaterialization,
		EntityKey:    "azure_resource_materialization:" + scopeValue.ScopeID,
		Reason:       "azure runtime relationship facts observed",
		FactID:       envelope.FactID,
		SourceSystem: cloudInventoryAdmissionSourceSystem(envelope),
	}, true
}
