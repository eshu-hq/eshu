package reducer

import "github.com/eshu-hq/eshu/go/internal/truth"

// appendAdditiveDomainDefinitions registers the source-neutral and graph-write
// domains that are only wired when their explicit handler dependencies are
// provided. Each registration is gated on its writer/loader because registering a
// domain without its handler would silently drop every intent it owns before the
// work reached the graph or read model. Keeping these here, separate from the
// default-catalog wiring in implementedDefaultDomainDefinitions, keeps each file
// focused and under the package size budget.
func appendAdditiveDomainDefinitions(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.TerraformBackendResolver != nil &&
		handlers.DriftEvidenceLoader != nil &&
		handlers.DriftLogger != nil {
		drift := configStateDriftDomainDefinition()
		drift.Handler = TerraformConfigStateDriftHandler{
			Resolver:       handlers.TerraformBackendResolver,
			EvidenceLoader: handlers.DriftEvidenceLoader,
			Instruments:    handlers.Instruments,
			Logger:         handlers.DriftLogger,
		}
		definitions = append(definitions, drift)
	}
	if handlers.FactLoader != nil && handlers.PackageCorrelationWriter != nil {
		packageSource := packageSourceCorrelationDomainDefinition()
		packageSource.Handler = PackageSourceCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.PackageCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, packageSource)
	}
	if handlers.FactLoader != nil && handlers.ContainerImageIdentityWriter != nil {
		imageIdentity := containerImageIdentityDomainDefinition()
		imageIdentity.Handler = ContainerImageIdentityHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.ContainerImageIdentityWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, imageIdentity)
	}
	if handlers.FactLoader != nil && handlers.CICDRunCorrelationWriter != nil {
		cicdRun := cicdRunCorrelationDomainDefinition()
		cicdRun.Handler = CICDRunCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.CICDRunCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, cicdRun)
	}
	if handlers.FactLoader != nil && handlers.ServiceCatalogCorrelationWriter != nil {
		serviceCatalog := serviceCatalogCorrelationDomainDefinition()
		serviceCatalog.Handler = ServiceCatalogCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.ServiceCatalogCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, serviceCatalog)
	}
	if handlers.FactLoader != nil && handlers.ObservabilityCoverageCorrelationWriter != nil {
		observability := observabilityCoverageCorrelationDomainDefinition()
		observability.Handler = ObservabilityCoverageCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.ObservabilityCoverageCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, observability)
	}
	if handlers.FactLoader != nil && handlers.KubernetesCorrelationWriter != nil {
		kubernetes := kubernetesCorrelationDomainDefinition()
		kubernetes.Handler = KubernetesCorrelationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.KubernetesCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, kubernetes)
	}
	if handlers.FactLoader != nil && handlers.SBOMAttestationAttachmentWriter != nil {
		attachments := sbomAttestationAttachmentDomainDefinition()
		attachments.Handler = SBOMAttestationAttachmentHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.SBOMAttestationAttachmentWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, attachments)
	}
	if handlers.FactLoader != nil && handlers.SupplyChainImpactWriter != nil {
		impact := supplyChainImpactDomainDefinition()
		impact.Handler = SupplyChainImpactHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.SupplyChainImpactWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, impact)
	}
	if handlers.FactLoader != nil && handlers.SecurityAlertReconciliationWriter != nil {
		securityAlerts := securityAlertReconciliationDomainDefinition()
		securityAlerts.Handler = SecurityAlertReconciliationHandler{
			FactLoader:  handlers.FactLoader,
			Writer:      handlers.SecurityAlertReconciliationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, securityAlerts)
	}
	if handlers.AWSCloudRuntimeDriftEvidenceLoader != nil &&
		handlers.AWSCloudRuntimeDriftWriter != nil {
		awsRuntimeDrift := awsCloudRuntimeDriftDomainDefinition()
		awsRuntimeDrift.Handler = AWSCloudRuntimeDriftHandler{
			EvidenceLoader: handlers.AWSCloudRuntimeDriftEvidenceLoader,
			Writer:         handlers.AWSCloudRuntimeDriftWriter,
			Instruments:    handlers.Instruments,
			Logger:         handlers.AWSCloudRuntimeDriftLogger,
		}
		definitions = append(definitions, awsRuntimeDrift)
	}
	if handlers.FactLoader != nil && handlers.CloudResourceNodeWriter != nil {
		awsResources := awsResourceMaterializationDomainDefinition()
		awsResources.Handler = AWSResourceMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.CloudResourceNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
		}
		definitions = append(definitions, awsResources)
	}
	if handlers.FactLoader != nil && handlers.EC2InstanceNodeWriter != nil {
		ec2Instances := ec2InstanceNodeMaterializationDomainDefinition()
		ec2Instances.Handler = EC2InstanceNodeMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.EC2InstanceNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
			Instruments:    handlers.Instruments,
		}
		definitions = append(definitions, ec2Instances)
	}
	if handlers.FactLoader != nil && handlers.KubernetesWorkloadNodeWriter != nil {
		kubernetesWorkloads := kubernetesWorkloadMaterializationDomainDefinition()
		kubernetesWorkloads.Handler = KubernetesWorkloadMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.KubernetesWorkloadNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
			Instruments:    handlers.Instruments,
		}
		definitions = append(definitions, kubernetesWorkloads)
	}
	definitions = appendSecurityGroupEndpointDomain(definitions, handlers)
	definitions = appendSecurityGroupReachabilityDomains(definitions, handlers)
	if handlers.FactLoader != nil && handlers.CloudResourceEdgeWriter != nil {
		awsRelationships := awsRelationshipMaterializationDomainDefinition()
		awsRelationships.Handler = AWSRelationshipMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.CloudResourceEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, awsRelationships)
	}
	if handlers.FactLoader != nil && handlers.ObservabilityCoverageEdgeWriter != nil {
		coverageEdges := observabilityCoverageMaterializationDomainDefinition()
		coverageEdges.Handler = ObservabilityCoverageMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.ObservabilityCoverageEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
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
	if handlers.FactLoader != nil && handlers.RDSPostureNodeWriter != nil {
		rdsPosture := rdsPostureMaterializationDomainDefinition()
		rdsPosture.Handler = RDSPostureMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			NodeWriter:           handlers.RDSPostureNodeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
		}
		definitions = append(definitions, rdsPosture)
	}
	if handlers.FactLoader != nil && handlers.KubernetesCorrelationEdgeWriter != nil {
		kubernetesEdges := kubernetesCorrelationMaterializationDomainDefinition()
		kubernetesEdges.Handler = KubernetesCorrelationMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.KubernetesCorrelationEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, kubernetesEdges)
	}
	if handlers.FactLoader != nil && handlers.IAMEscalationEdgeWriter != nil {
		iamEscalation := iamEscalationMaterializationDomainDefinition()
		iamEscalation.Handler = IAMEscalationMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			Writer:               handlers.IAMEscalationEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, iamEscalation)
	}
	if handlers.IncidentRoutingEvidenceLoader != nil && handlers.IncidentRoutingEvidenceWriter != nil {
		incidentRouting := incidentRoutingMaterializationDomainDefinition()
		incidentRouting.Handler = IncidentRoutingMaterializationHandler{
			Loader:               handlers.IncidentRoutingEvidenceLoader,
			Writer:               handlers.IncidentRoutingEvidenceWriter,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, incidentRouting)
	}
	if handlers.DeployableUnitCorrelationHandler != nil {
		definitions = append(definitions, DomainDefinition{
			Domain:  DomainDeployableUnitCorrelation,
			Summary: "correlate deployable-unit candidates across sources before workload admission",
			Ownership: OwnershipShape{
				CrossSource:    true,
				CrossScope:     true,
				CanonicalWrite: true,
			},
			TruthContract: truth.Contract{
				CanonicalKind: "deployable_unit_correlation",
				SourceLayers: []truth.Layer{
					truth.LayerSourceDeclaration,
				},
			},
			Handler: handlers.DeployableUnitCorrelationHandler,
		})
	}

	return definitions
}
