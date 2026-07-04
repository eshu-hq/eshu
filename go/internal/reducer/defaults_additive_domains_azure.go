// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendAzureResourceMaterializationDomain registers the Azure cloud-resource
// node materialization domain when its handler dependencies are wired. It uses
// the provider-neutral CloudResourceNodeWriter so Azure resources land on the
// same canonical substrate as AWS and GCP without a new node writer.
func appendAzureResourceMaterializationDomain(
	definitions []DomainDefinition,
	handlers DefaultHandlers,
) []DomainDefinition {
	if handlers.FactLoader == nil || handlers.CloudResourceNodeWriter == nil {
		return definitions
	}
	azureResources := azureResourceMaterializationDomainDefinition()
	azureResources.Handler = AzureResourceMaterializationHandler{
		FactLoader:     handlers.FactLoader,
		NodeWriter:     handlers.CloudResourceNodeWriter,
		PhasePublisher: handlers.GraphProjectionPhasePublisher,
		PresenceWriter: handlers.EndpointPresenceWriter,
		Instruments:    handlers.Instruments,
	}
	return append(definitions, azureResources)
}

// appendAzureRelationshipMaterializationDomain registers the Azure relationship
// edge projection domain only when the explicit Azure edge writer is wired. The
// handler gates on Azure node readiness, so managedBy edges never resolve
// against uncommitted CloudResource nodes.
func appendAzureRelationshipMaterializationDomain(
	definitions []DomainDefinition,
	handlers DefaultHandlers,
) []DomainDefinition {
	if handlers.FactLoader == nil || handlers.AzureCloudResourceEdgeWriter == nil {
		return definitions
	}
	azureRelationships := azureRelationshipMaterializationDomainDefinition()
	azureRelationships.Handler = AzureRelationshipMaterializationHandler{
		FactLoader:           handlers.FactLoader,
		EdgeWriter:           handlers.AzureCloudResourceEdgeWriter,
		ReadinessLookup:      handlers.ReadinessLookup,
		PriorGenerationCheck: handlers.PriorGenerationCheck,
		Tracer:               handlers.Tracer,
		Instruments:          handlers.Instruments,
	}
	return append(definitions, azureRelationships)
}
