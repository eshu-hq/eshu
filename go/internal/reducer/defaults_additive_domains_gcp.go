// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendGCPResourceMaterializationDomain registers the GCP cloud-resource node
// materialization domain when its handler dependencies are wired. It reuses the
// provider-neutral CloudResourceNodeWriter the AWS path already wires, so GCP
// resources land as canonical CloudResource graph nodes without a new writer.
// The registration is gated (additive) so a deployment without the node writer
// never enqueues intents that would silently drop. See
// docs/internal/gcp-cloud-resource-materialization-design.md.
func appendGCPResourceMaterializationDomain(
	definitions []DomainDefinition,
	handlers DefaultHandlers,
) []DomainDefinition {
	if handlers.FactLoader == nil || handlers.CloudResourceNodeWriter == nil {
		return definitions
	}
	gcpResources := gcpResourceMaterializationDomainDefinition()
	gcpResources.Handler = GCPResourceMaterializationHandler{
		FactLoader:     handlers.FactLoader,
		NodeWriter:     handlers.CloudResourceNodeWriter,
		PhasePublisher: handlers.GraphProjectionPhasePublisher,
		PresenceWriter: handlers.EndpointPresenceWriter,
		Instruments:    handlers.Instruments,
	}
	return append(definitions, gcpResources)
}

// appendGCPRelationshipMaterializationDomain registers the GCP relationship edge
// projection domain when its handler dependencies are wired. The handler gates
// on the GCP node canonical-nodes phase via ReadinessLookup so edges never
// resolve against uncommitted GCP nodes. The registration is gated (additive) so
// a deployment without the GCP edge writer never enqueues intents that would
// silently drop. See
// docs/internal/gcp-cloud-relationship-edge-materialization-design.md.
func appendGCPRelationshipMaterializationDomain(
	definitions []DomainDefinition,
	handlers DefaultHandlers,
) []DomainDefinition {
	if handlers.FactLoader == nil || handlers.GCPCloudResourceEdgeWriter == nil {
		return definitions
	}
	gcpRelationships := gcpRelationshipMaterializationDomainDefinition()
	gcpRelationships.Handler = GCPRelationshipMaterializationHandler{
		FactLoader:           handlers.FactLoader,
		EdgeWriter:           handlers.GCPCloudResourceEdgeWriter,
		ReadinessLookup:      handlers.ReadinessLookup,
		PriorGenerationCheck: handlers.PriorGenerationCheck,
		Ledger:               handlers.ProjectedSourceLedger,
		Tracer:               handlers.Tracer,
		Instruments:          handlers.Instruments,
	}
	return append(definitions, gcpRelationships)
}
