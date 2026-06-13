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
	}
	return append(definitions, gcpResources)
}
