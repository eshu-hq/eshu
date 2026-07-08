// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/truth"

// appendIncidentAndCodeEvidenceAdditiveDomains registers the incident-routing,
// code-evidence, and deployable-unit correlation domains: incident routing,
// code taint evidence, code interprocedural evidence, code function summaries,
// incident-repository correlation, and deployable-unit correlation. Each
// registration is gated on its explicit evidence loader / writer (or handler)
// so the runtime never registers a domain without a durable publication path.
// Append order matches the original monolithic appendAdditiveDomainDefinitions;
// registration is keyed by Domain so order is not runtime-observable.
func appendIncidentAndCodeEvidenceAdditiveDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
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
			Ledger:               handlers.CodeTaintEvidenceProjectedNodeLedger,
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
			Ledger:               handlers.CodeInterprocProjectedEdgeLedger,
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
