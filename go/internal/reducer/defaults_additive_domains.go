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
	if handlers.EshuSearchDocumentSourceLoader != nil && handlers.EshuSearchDocumentWriter != nil {
		searchDocument := eshuSearchDocumentDomainDefinition()
		searchDocument.Handler = EshuSearchDocumentHandler{
			Loader:      handlers.EshuSearchDocumentSourceLoader,
			Writer:      handlers.EshuSearchDocumentWriter,
			Instruments: handlers.Instruments,
			Logger:      handlers.EshuSearchDocumentLogger,
		}
		definitions = append(definitions, searchDocument)
	}
	if handlers.FactLoader != nil && handlers.PackageCorrelationWriter != nil {
		packageSource := packageSourceCorrelationDomainDefinition()
		packageSource.Handler = PackageSourceCorrelationHandler{
			FactLoader:                 handlers.FactLoader,
			Writer:                     handlers.PackageCorrelationWriter,
			Instruments:                handlers.Instruments,
			AdmissionDecisionWriter:    handlers.AdmissionDecisionWriter,
			AdmissionDecisionNow:       handlers.AdmissionDecisionNow,
			RepoDependencyIntentWriter: handlers.RepoDependencyIntentWriter,
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
			FactLoader:                   handlers.FactLoader,
			Writer:                       handlers.ServiceCatalogCorrelationWriter,
			MaterializationWriter:        handlers.ServiceMaterializationWriter,
			DeploymentRelationshipLoader: serviceCatalogDeploymentRelationshipLoader(handlers),
			RuntimeInstanceLoader:        serviceCatalogRuntimeInstanceLoader(handlers),
			DocumentationEvidenceLoader:  serviceCatalogDocumentationEvidenceLoader(handlers),
			IncidentEvidenceLoader:       serviceCatalogIncidentEvidenceLoader(handlers),
			VulnerabilityEvidenceLoader:  serviceCatalogVulnerabilityEvidenceLoader(handlers),
			Instruments:                  handlers.Instruments,
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
	if handlers.SecretsIAMTrustChainEvidenceLoader != nil && handlers.SecretsIAMTrustChainWriter != nil {
		secretsIAM := secretsIAMTrustChainDomainDefinition()
		secretsIAM.Handler = SecretsIAMTrustChainHandler{
			EvidenceLoader: handlers.SecretsIAMTrustChainEvidenceLoader,
			Writer:         handlers.SecretsIAMTrustChainWriter,
			Instruments:    handlers.Instruments,
		}
		definitions = append(definitions, secretsIAM)
	}
	if handlers.FactLoader != nil && handlers.SecretsIAMGraphWriter != nil {
		secretsIAMGraph := secretsIAMGraphProjectionDomainDefinition()
		secretsIAMGraph.Handler = SecretsIAMGraphProjectionHandler{
			FactLoader:           handlers.FactLoader,
			Writer:               handlers.SecretsIAMGraphWriter,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			PresenceLookup:       handlers.EndpointPresenceLookup,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, secretsIAMGraph)
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
	if handlers.MultiCloudRuntimeDriftEvidenceLoader != nil &&
		handlers.MultiCloudRuntimeDriftWriter != nil {
		multiCloudDrift := multiCloudRuntimeDriftDomainDefinition()
		multiCloudDrift.Handler = MultiCloudRuntimeDriftHandler{
			EvidenceLoader: handlers.MultiCloudRuntimeDriftEvidenceLoader,
			Writer:         handlers.MultiCloudRuntimeDriftWriter,
			Instruments:    handlers.Instruments,
			Logger:         handlers.MultiCloudRuntimeDriftLogger,
		}
		definitions = append(definitions, multiCloudDrift)
	}
	if handlers.CloudInventoryEvidenceLoader != nil && handlers.CloudInventoryAdmissionWriter != nil {
		cloudInventory := cloudInventoryAdmissionDomainDefinition()
		cloudInventory.Handler = CloudInventoryAdmissionHandler{
			EvidenceLoader:               handlers.CloudInventoryEvidenceLoader,
			Writer:                       handlers.CloudInventoryAdmissionWriter,
			GenerationCheck:              handlers.CloudInventoryGenerationCheck,
			TagEvidenceLoader:            handlers.CloudInventoryTagEvidenceLoader,
			IdentityPolicyEvidenceLoader: handlers.CloudInventoryIdentityPolicyEvidenceLoader,
			ResourceChangeEvidenceLoader: handlers.CloudInventoryResourceChangeEvidenceLoader,
			Instruments:                  handlers.Instruments,
			AdmissionDecisionWriter:      handlers.AdmissionDecisionWriter,
			AdmissionDecisionNow:         handlers.AdmissionDecisionNow,
		}
		definitions = append(definitions, cloudInventory)
	}
	if handlers.FactLoader != nil && handlers.CloudResourceNodeWriter != nil {
		awsResources := awsResourceMaterializationDomainDefinition()
		awsResources.Handler = AWSResourceMaterializationHandler{
			FactLoader:     handlers.FactLoader,
			NodeWriter:     handlers.CloudResourceNodeWriter,
			PhasePublisher: handlers.GraphProjectionPhasePublisher,
			PresenceWriter: handlers.EndpointPresenceWriter,
		}
		definitions = append(definitions, awsResources)
	}
	definitions = appendGCPResourceMaterializationDomain(definitions, handlers)
	definitions = appendGCPRelationshipMaterializationDomain(definitions, handlers)
	definitions = appendAzureResourceMaterializationDomain(definitions, handlers)
	definitions = appendAzureRelationshipMaterializationDomain(definitions, handlers)
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
			PresenceWriter: handlers.EndpointPresenceWriter,
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
	if handlers.FactLoader != nil && handlers.WorkloadCloudRelationshipEdgeWriter != nil {
		workloadCloud := workloadCloudRelationshipMaterializationDomainDefinition()
		workloadCloud.Handler = WorkloadCloudRelationshipMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.WorkloadCloudRelationshipEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
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
		}
		definitions = append(definitions, rdsPosture)
	}
	if handlers.FactLoader != nil && handlers.EC2UsesProfileEdgeWriter != nil {
		ec2UsesProfile := ec2UsesProfileMaterializationDomainDefinition()
		ec2UsesProfile.Handler = EC2UsesProfileMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.EC2UsesProfileEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, ec2UsesProfile)
	}
	if handlers.FactLoader != nil && handlers.IAMInstanceProfileRoleEdgeWriter != nil {
		profileRole := iamInstanceProfileRoleMaterializationDomainDefinition()
		profileRole.Handler = IAMInstanceProfileRoleMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			EdgeWriter:           handlers.IAMInstanceProfileRoleEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, profileRole)
	}
	if handlers.FactLoader != nil && handlers.EC2BlockDeviceKMSPostureNodeWriter != nil {
		ec2BlockDeviceKMS := ec2BlockDeviceKMSPostureMaterializationDomainDefinition()
		ec2BlockDeviceKMS.Handler = EC2BlockDeviceKMSPostureMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			NodeWriter:           handlers.EC2BlockDeviceKMSPostureNodeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, ec2BlockDeviceKMS)
	}
	if handlers.FactLoader != nil && handlers.S3InternetExposureNodeWriter != nil {
		s3Exposure := s3InternetExposureMaterializationDomainDefinition()
		s3Exposure.Handler = S3InternetExposureMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			NodeWriter:           handlers.S3InternetExposureNodeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, s3Exposure)
	}
	if handlers.FactLoader != nil && handlers.EC2InternetExposureNodeWriter != nil {
		ec2Exposure := ec2InternetExposureMaterializationDomainDefinition()
		ec2Exposure.Handler = EC2InternetExposureMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			NodeWriter:           handlers.EC2InternetExposureNodeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, ec2Exposure)
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
	if handlers.FactLoader != nil && handlers.IAMCanPerformEdgeWriter != nil {
		iamCanPerform := iamCanPerformMaterializationDomainDefinition()
		iamCanPerform.Handler = IAMCanPerformMaterializationHandler{
			FactLoader:           handlers.FactLoader,
			Writer:               handlers.IAMCanPerformEdgeWriter,
			ReadinessLookup:      handlers.ReadinessLookup,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Tracer:               handlers.Tracer,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, iamCanPerform)
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
	if handlers.CodeTaintEvidenceLoader != nil && handlers.CodeTaintEvidenceWriter != nil {
		codeTaint := codeTaintEvidenceDomainDefinition()
		codeTaint.Handler = CodeTaintEvidenceMaterializationHandler{
			Loader:               handlers.CodeTaintEvidenceLoader,
			Writer:               handlers.CodeTaintEvidenceWriter,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, codeTaint)
	}
	if handlers.CodeInterprocEvidenceLoader != nil && handlers.CodeInterprocEvidenceWriter != nil {
		codeInterproc := codeInterprocEvidenceDomainDefinition()
		codeInterproc.Handler = CodeInterprocEvidenceMaterializationHandler{
			Loader:               handlers.CodeInterprocEvidenceLoader,
			Writer:               handlers.CodeInterprocEvidenceWriter,
			PriorGenerationCheck: handlers.PriorGenerationCheck,
			Instruments:          handlers.Instruments,
		}
		definitions = append(definitions, codeInterproc)
	}
	if handlers.CodeFunctionSummaryLoader != nil && handlers.CodeFunctionSummaryWriter != nil {
		codeFunctionSummary := codeFunctionSummaryDomainDefinition()
		codeFunctionSummary.Handler = CodeFunctionSummaryMaterializationHandler{
			Loader:                  handlers.CodeFunctionSummaryLoader,
			Writer:                  handlers.CodeFunctionSummaryWriter,
			SourceLoader:            handlers.CodeFunctionSourceLoader,
			SourceWriter:            handlers.CodeFunctionSourceWriter,
			GraphIDLoader:           handlers.CodeFunctionGraphIDLoader,
			GraphIDWriter:           handlers.CodeFunctionGraphIDWriter,
			ValueFlowFixpointWriter: handlers.ValueFlowFixpointProjector,
			Instruments:             handlers.Instruments,
		}
		definitions = append(definitions, codeFunctionSummary)
	}
	if handlers.AppliedPagerDutyServiceRoutingLoader != nil && handlers.IncidentRepositoryCorrelationWriter != nil {
		incidentRepoCorrelation := incidentRepositoryCorrelationDomainDefinition()
		incidentRepoCorrelation.Handler = IncidentRepositoryCorrelationHandler{
			Loader:      handlers.AppliedPagerDutyServiceRoutingLoader,
			Resolver:    handlers.BackendRepositoryResolver,
			Writer:      handlers.IncidentRepositoryCorrelationWriter,
			Instruments: handlers.Instruments,
		}
		definitions = append(definitions, incidentRepoCorrelation)
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

// serviceCatalogDeploymentRelationshipLoader returns the repository-scoped
// resolved-relationship loader used to materialize the service deployment
// evidence family (#1985), or nil when the deployment family cannot be sourced.
// The family is only wired when both the service generation lineage writer is
// present and the configured resolved-relationship loader exposes the
// repository-scoped read, so deployment evidence is purely additive and never
// blocks the Stage-1 ownership lineage. The same nil-tolerant assertion pattern
// is used by the correlated workload projection input loader.
func serviceCatalogDeploymentRelationshipLoader(
	handlers DefaultHandlers,
) RepositoryScopedResolvedRelationshipLoader {
	if handlers.ServiceMaterializationWriter == nil || handlers.ResolvedRelationshipLoader == nil {
		return nil
	}
	repoScoped, ok := handlers.ResolvedRelationshipLoader.(RepositoryScopedResolvedRelationshipLoader)
	if !ok {
		return nil
	}
	return repoScoped
}

// serviceCatalogRuntimeInstanceLoader returns the repository-scoped runtime
// instance loader used to materialize the service runtime evidence family
// (#1986), or nil when the family cannot be sourced. The family is only wired
// when both the service generation lineage writer is present and a runtime
// instance loader is configured, so runtime evidence is purely additive and never
// blocks the ownership/deployment lineage.
func serviceCatalogRuntimeInstanceLoader(
	handlers DefaultHandlers,
) RepositoryScopedRuntimeInstanceLoader {
	if handlers.ServiceMaterializationWriter == nil || handlers.ServiceRuntimeInstanceLoader == nil {
		return nil
	}
	return handlers.ServiceRuntimeInstanceLoader
}

// serviceCatalogDocumentationEvidenceLoader returns the service-scoped
// documentation evidence loader used to materialize the service docs evidence
// family (#1988), or nil when the family cannot be sourced. The family is only
// wired when both the service generation lineage writer is present and a
// documentation evidence loader is configured, so docs evidence is purely
// additive and never blocks the ownership/deployment/runtime/dependencies
// lineage.
func serviceCatalogDocumentationEvidenceLoader(
	handlers DefaultHandlers,
) ServiceScopedDocumentationEvidenceLoader {
	if handlers.ServiceMaterializationWriter == nil || handlers.ServiceDocumentationEvidenceLoader == nil {
		return nil
	}
	return handlers.ServiceDocumentationEvidenceLoader
}

// serviceCatalogVulnerabilityEvidenceLoader returns the repository-scoped
// supply-chain advisory loader used to materialize the service vulnerabilities
// evidence family (#1990, #2127), or nil when the family cannot be sourced. The
// family is only wired when both the service generation lineage writer is present
// and a supply-chain advisory loader is configured, so vulnerabilities evidence is
// purely additive and never blocks the ownership/deployment/runtime/dependencies/
// docs lineage.
func serviceCatalogVulnerabilityEvidenceLoader(
	handlers DefaultHandlers,
) ServiceVulnerabilityAdvisoryLoader {
	if handlers.ServiceMaterializationWriter == nil || handlers.ServiceVulnerabilityAdvisoryLoader == nil {
		return nil
	}
	return handlers.ServiceVulnerabilityAdvisoryLoader
}
