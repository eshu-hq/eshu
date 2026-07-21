// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
				InstanceRetractionLookup:     handlers.InstanceRetractionLookup,
				PhasePublisher:               handlers.GraphProjectionPhasePublisher,
				RepairQueue:                  handlers.GraphProjectionRepairQueue,
				// The handles_route (repo_id, path) presence writer (#2809) is wired
				// independently of the secrets/IAM uid presence writer, so workload
				// materialization records endpoint presence whenever the handles_route
				// gate is enabled — never coupled to the secrets/IAM flag.
				EndpointPresenceWriter: handlers.APIEndpointRepoPathPresenceWriter,
			}
		case DomainCodeCallMaterialization:
			def.Handler = CodeCallMaterializationHandler{
				FactLoader:   handlers.FactLoader,
				IntentWriter: handlers.CodeCallIntentWriter,
				Instruments:  handlers.Instruments,
			}
		case DomainPlatformInfraMaterialization:
			def.Handler = PlatformInfraMaterializationHandler{
				FactLoader:                 handlers.FactLoader,
				InfrastructureMaterializer: handlers.InfrastructurePlatformMaterializer,
				PlatformGraphLocker:        handlers.PlatformGraphLocker,
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
				FactLoader:   handlers.FactLoader,
				IntentWriter: handlers.SQLRelationshipIntentWriter,
			}
		case DomainShellExecMaterialization:
			def.Handler = ShellExecMaterializationHandler{
				FactLoader:   handlers.FactLoader,
				IntentWriter: handlers.ShellExecIntentWriter,
			}
		case DomainInheritanceMaterialization:
			def.Handler = InheritanceMaterializationHandler{
				FactLoader:   handlers.FactLoader,
				IntentWriter: handlers.InheritanceIntentWriter,
			}
		case DomainDocumentationMaterialization:
			def.Handler = DocumentationEdgeMaterializationHandler{
				FactLoader:           handlers.FactLoader,
				EdgeWriter:           handlers.DocumentationEdgeWriter,
				PriorGenerationCheck: handlers.PriorGenerationCheck,
				Instruments:          handlers.Instruments,
			}
		case DomainRationaleMaterialization:
			def.Handler = RationaleEdgeMaterializationHandler{
				FactLoader:   handlers.FactLoader,
				IntentWriter: handlers.RationaleEdgeIntentWriter,
			}
		case DomainCodeownersOwnership:
			def.Handler = CodeownersOwnershipEdgeMaterializationHandler{
				FactLoader:           handlers.FactLoader,
				EdgeWriter:           handlers.CodeownersOwnershipEdgeWriter,
				PriorGenerationCheck: handlers.PriorGenerationCheck,
				Instruments:          handlers.Instruments,
			}
		}
		definitions = append(definitions, def)
	}
	return appendAdditiveDomainDefinitions(definitions, handlers)
}
