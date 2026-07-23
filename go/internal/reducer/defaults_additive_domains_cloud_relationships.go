// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendCloudRelationshipAdditiveDomains registers the cloud-relationship edge
// and posture-node domains that read back committed graph state through the
// readiness lookup and prior-generation check: AWS relationships, workload-cloud
// relationships, observability-coverage edges, IAM CAN_ASSUME edges, S3 LOGS_TO
// edges, S3 external-principal grants, and RDS posture nodes. Each registration
// is gated on the fact loader plus its edge/node writer so the runtime never
// registers a domain without a durable publication path. Append order matches
// the original monolithic appendAdditiveDomainDefinitions; registration is keyed
// by Domain so order is not runtime-observable.
func appendCloudRelationshipAdditiveDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.FactLoader != nil && handlers.CloudResourceEdgeWriter != nil {
		awsRelationships := awsRelationshipMaterializationDomainDefinition()
		awsRelationships.Handler = AWSRelationshipMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.CloudResourceEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Ledger:               handlers.ProjectedSourceLedger,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, awsRelationships)
	}
	if handlers.FactLoader != nil && handlers.CloudResourceContainerImageEdgeWriter != nil {
		awsCloudImage := awsCloudImageMaterializationDomainDefinition()
		awsCloudImage.Handler = AWSCloudImageMaterializationHandler{
			FactLoader:              handlers.FactLoader,
			EdgeWriter:              handlers.CloudResourceContainerImageEdgeWriter,
			ReadinessLookup:         handlers.ReadinessLookup,
			PriorGenerationCheck:    handlers.PriorGenerationCheck,
			ContainerImageExistence: handlers.ContainerImageExistence,
			Tracer:                  handlers.Tracer,
			Instruments:             handlers.Instruments,
		}
		definitions = append(definitions, awsCloudImage)
	}
	if handlers.FactLoader != nil && handlers.WorkloadCloudRelationshipEdgeWriter != nil {
		workloadCloud := workloadCloudRelationshipMaterializationDomainDefinition()
		workloadCloud.Handler = WorkloadCloudRelationshipMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.WorkloadCloudRelationshipEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, workloadCloud)
	}
	if handlers.FactLoader != nil && handlers.ObservabilityCoverageEdgeWriter != nil {
		coverageEdges := observabilityCoverageMaterializationDomainDefinition()
		coverageEdges.Handler = ObservabilityCoverageMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.ObservabilityCoverageEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Ledger:               handlers.ProjectedSourceLedger,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, coverageEdges)
	}
	if handlers.FactLoader != nil && handlers.IAMCanAssumeEdgeWriter != nil {
		iamCanAssume := iamCanAssumeMaterializationDomainDefinition()
		iamCanAssume.Handler = IAMCanAssumeMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.IAMCanAssumeEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, iamCanAssume)
	}
	if handlers.FactLoader != nil && handlers.S3LogsToEdgeWriter != nil {
		s3LogsTo := s3LogsToMaterializationDomainDefinition()
		s3LogsTo.Handler = S3LogsToMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.S3LogsToEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, s3LogsTo)
	}
	if handlers.FactLoader != nil && handlers.S3ExternalPrincipalGrantWriter != nil {
		s3Grant := s3ExternalPrincipalGrantMaterializationDomainDefinition()
		s3Grant.Handler = S3ExternalPrincipalGrantMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			GrantWriter:          handlers.S3ExternalPrincipalGrantWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, s3Grant)
	}
	if handlers.FactLoader != nil && handlers.RDSPostureNodeWriter != nil {
		rdsPosture := rdsPostureMaterializationDomainDefinition()
		rdsPosture.Handler = RDSPostureMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			NodeWriter:           handlers.RDSPostureNodeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, rdsPosture)
	}
	return definitions
}
