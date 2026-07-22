// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendCorrelationCoreAdditiveDomains registers the source-neutral correlation
// and evidence domains that depend on the fact loader plus a single dedicated
// writer: config-state drift, the eshu search document, package-source and
// code-import correlation, container image identity, CI/CD run correlation, and
// the service catalog. Each registration is gated on its writer/loader so the
// runtime never registers a domain that has no durable publication path. The
// helper preserves the append order of the original monolithic
// appendAdditiveDomainDefinitions; registration is keyed by Domain so order is
// not runtime-observable, but the verbatim order keeps the split reviewable.
func appendCorrelationCoreAdditiveDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.TerraformBackendResolver != nil &&
		handlers.DriftEvidenceLoader != nil &&
		handlers.DriftWriter != nil &&
		handlers.DriftLogger != nil {
		drift := configStateDriftDomainDefinition()
		drift.Handler = TerraformConfigStateDriftHandler{
			Resolver:       handlers.TerraformBackendResolver,
			EvidenceLoader: handlers.DriftEvidenceLoader,
			Instruments:    handlers.Instruments,
			Logger:         handlers.DriftLogger,
			Writer:         handlers.DriftWriter,
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
	if ownershipLoader, ok := codeImportOwnershipLoader(handlers.FactLoader); ok &&
		handlers.RepoDependencyIntentWriter != nil {
		codeImport := codeImportRepoEdgeDomainDefinition()
		codeImport.Handler = CodeImportRepoEdgeHandler{
			FactLoader:                 handlers.FactLoader,
			OwnershipLoader:            ownershipLoader,
			RepoDependencyIntentWriter: handlers.RepoDependencyIntentWriter,
			Instruments:                handlers.Instruments,
			Tracer:                     handlers.Tracer,
		}
		definitions = append(definitions, codeImport)
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
	return definitions
}
