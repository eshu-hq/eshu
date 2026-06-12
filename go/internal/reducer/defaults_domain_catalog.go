package reducer

// implementedDefaultDomainDefinitions binds the reducer-owned handlers to the
// default domain catalog. It attaches the graph-projection and materialization
// handlers that are always present, then delegates to
// appendAdditiveDomainDefinitions for the source-neutral domains that are only
// wired when their explicit handler dependencies are provided. It lives in its
// own file to keep defaults.go under the package size budget.
func implementedDefaultDomainDefinitions(handlers DefaultHandlers) []DomainDefinition {
	definitions := make([]DomainDefinition, 0, len(DefaultDomainDefinitions())+1)
	for _, def := range DefaultDomainDefinitions() {
		switch def.Domain {
		case DomainWorkloadIdentity:
			def.Handler = WorkloadIdentityHandler{
				Writer:         handlers.WorkloadIdentityWriter,
				PhasePublisher: handlers.GraphProjectionPhasePublisher,
			}
		case DomainCloudAssetResolution:
			def.Handler = CloudAssetResolutionHandler{
				Writer:         handlers.CloudAssetResolutionWriter,
				PhasePublisher: handlers.GraphProjectionPhasePublisher,
			}
		case DomainDeploymentMapping:
			var crossRepoResolver *CrossRepoRelationshipHandler
			if handlers.EvidenceFactLoader != nil && handlers.RepoDependencyIntentWriter != nil {
				crossRepoResolver = &CrossRepoRelationshipHandler{
					EvidenceLoader:    handlers.EvidenceFactLoader,
					Assertions:        handlers.AssertionLoader,
					Persister:         handlers.ResolutionPersister,
					IntentWriter:      handlers.RepoDependencyIntentWriter,
					ReadinessLookup:   handlers.ReadinessLookup,
					ReadinessPrefetch: handlers.ReadinessPrefetch,
					Tracer:            handlers.Tracer,
					Instruments:       handlers.Instruments,
				}
			}
			def.Handler = PlatformMaterializationHandler{
				Writer:                          handlers.PlatformMaterializationWriter,
				FactLoader:                      handlers.FactLoader,
				InfrastructureMaterializer:      handlers.InfrastructurePlatformMaterializer,
				PlatformGraphLocker:             handlers.PlatformGraphLocker,
				CrossRepoResolver:               crossRepoResolver,
				WorkloadMaterializationReplayer: handlers.WorkloadMaterializationReplayer,
				PhasePublisher:                  handlers.GraphProjectionPhasePublisher,
			}
		case DomainWorkloadMaterialization:
			def.Handler = WorkloadMaterializationHandler{
				FactLoader:                   handlers.FactLoader,
				ResolvedLoader:               handlers.ResolvedRelationshipLoader,
				InputLoader:                  handlers.WorkloadProjectionInputLoader,
				InfrastructurePlatformLookup: handlers.InfrastructurePlatformLookup,
				Materializer:                 handlers.WorkloadMaterializer,
				DependencyLookup:             handlers.WorkloadDependencyLookup,
				WorkloadDependencyEdgeWriter: handlers.WorkloadDependencyEdgeWriter,
				PhasePublisher:               handlers.GraphProjectionPhasePublisher,
			}
		case DomainCodeCallMaterialization:
			def.Handler = CodeCallMaterializationHandler{
				FactLoader:   handlers.FactLoader,
				IntentWriter: handlers.CodeCallIntentWriter,
			}
		case DomainSemanticEntityMaterialization:
			def.Handler = SemanticEntityMaterializationHandler{
				FactLoader:           handlers.FactLoader,
				Writer:               handlers.SemanticEntityWriter,
				PriorGenerationCheck: handlers.PriorGenerationCheck,
				PhasePublisher:       handlers.GraphProjectionPhasePublisher,
				RepairQueue:          handlers.GraphProjectionRepairQueue,
			}
		case DomainSQLRelationshipMaterialization:
			def.Handler = SQLRelationshipMaterializationHandler{
				FactLoader:           handlers.FactLoader,
				EdgeWriter:           handlers.SQLRelationshipEdgeWriter,
				PriorGenerationCheck: handlers.PriorGenerationCheck,
			}
		case DomainInheritanceMaterialization:
			def.Handler = InheritanceMaterializationHandler{
				FactLoader:           handlers.FactLoader,
				EdgeWriter:           handlers.InheritanceEdgeWriter,
				PriorGenerationCheck: handlers.PriorGenerationCheck,
			}
		case DomainDocumentationMaterialization:
			def.Handler = DocumentationEdgeMaterializationHandler{
				FactLoader:           handlers.FactLoader,
				EdgeWriter:           handlers.DocumentationEdgeWriter,
				PriorGenerationCheck: handlers.PriorGenerationCheck,
			}
		}
		definitions = append(definitions, def)
	}
	return appendAdditiveDomainDefinitions(definitions, handlers)
}
