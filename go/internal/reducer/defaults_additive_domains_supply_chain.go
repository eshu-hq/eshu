// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendSupplyChainCorrelationAdditiveDomains registers the observability,
// kubernetes, and supply-chain correlation domains: observability coverage,
// kubernetes correlation, SBOM attestation attachment, supply-chain impact, and
// security-alert reconciliation. Each registration is gated on the fact loader
// plus its dedicated writer so the runtime never registers a domain without a
// durable publication path. Append order matches the original monolithic
// appendAdditiveDomainDefinitions; registration is keyed by Domain so order is
// not runtime-observable.
func appendSupplyChainCorrelationAdditiveDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
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
	return definitions
}
