// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azure

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildAzureResourceMaterializationReducerIntent enqueues one reducer intent
// that materializes azure_cloud_resource facts into canonical CloudResource
// nodes. The entity key is the readiness unit consumed by Azure relationship
// projection.
func BuildResourceMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	lookup projectorintent.FactLookup,
) (ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.AzureCloudResourceFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainAzureResourceMaterialization,
		EntityKey:    "azure_resource_materialization:" + scopeValue.ScopeID,
		Reason:       "azure runtime resource facts observed",
		FactID:       envelope.FactID,
		SourceSystem: cloudInventoryAdmissionSourceSystem(envelope),
	}, true
}
