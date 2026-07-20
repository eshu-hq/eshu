// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendAdditiveDomainDefinitions registers the source-neutral and graph-write
// domains that are only wired when their explicit handler dependencies are
// provided. Each registration is gated on its writer/loader because registering a
// domain without its handler would silently drop every intent it owns before the
// work reached the graph or read model. Keeping these here, separate from the
// default-catalog wiring in implementedDefaultDomainDefinitions, keeps each file
// focused and under the package size budget.
//
// The per-domain registrations are grouped into themed sibling helpers
// (defaults_additive_domains_*.go) so no single file or function exceeds the
// repository size budget. The helpers run in the same sequence the registrations
// originally appeared; registration is keyed by Domain in Registry.Register, so
// the append order is not runtime-observable, but the verbatim order keeps the
// split reviewable against the original monolith.
func appendAdditiveDomainDefinitions(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	definitions = appendCorrelationCoreAdditiveDomains(definitions, handlers)
	definitions = appendSupplyChainCorrelationAdditiveDomains(definitions, handlers)
	definitions = appendSecretsAndDriftAdditiveDomains(definitions, handlers)
	definitions = appendCloudResourceNodeAdditiveDomains(definitions, handlers)
	definitions = appendCloudRelationshipAdditiveDomains(definitions, handlers)
	definitions = appendCloudPostureEdgeAdditiveDomains(definitions, handlers)
	definitions = appendIncidentAndCodeEvidenceAdditiveDomains(definitions, handlers)
	definitions = appendCrossplaneAdditiveDomains(definitions, handlers)
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
