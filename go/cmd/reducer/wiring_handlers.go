// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// buildReducerDriftHandlers assembles the provider config-vs-state and
// cloud-runtime drift adapters (reducer.DriftHandlers; see
// internal/reducer/defaults_handlers.go). All three terraform members must be
// non-nil for the registry to register DomainConfigStateDrift. The multi-cloud
// runtime drift wiring is constructed here because it feeds only this group.
func buildReducerDriftHandlers(
	database postgres.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	getenv func(string) string,
) reducer.DriftHandlers {
	// Multi-cloud runtime drift wiring (issues #1997, #1998); see
	// multiCloudRuntimeDriftWiring for the uid-keyed join contract.
	multiCloudRuntimeDriftEvidenceLoader, multiCloudRuntimeDriftWriter, multiCloudRuntimeDriftLogger := multiCloudRuntimeDriftWiring(database, tracer, instruments, logger)
	return reducer.DriftHandlers{
		TerraformBackendResolver: tfstatebackend.NewResolver(
			postgres.PostgresTerraformBackendQuery{DB: database},
		),
		DriftEvidenceLoader: postgres.PostgresDriftEvidenceLoader{
			DB:               database,
			Tracer:           tracer,
			Logger:           logger,
			PriorConfigDepth: parsePriorConfigDepth(getenv(driftPriorConfigDepthEnv), logger),
			// Instruments drives eshu_dp_drift_unresolved_module_calls_total
			// for the module-aware drift join (issue #169). Nil-safe in the
			// loader itself; passing it here keeps the counter wired in
			// production runs.
			Instruments: instruments,
		},
		DriftLogger: logger,
		// AWS runtime drift joins current AWS resource facts to active
		// Terraform-state resources by ARN, then resolves the state backend to
		// the owning config snapshot before classifying unmanaged resources.
		AWSCloudRuntimeDriftEvidenceLoader: postgres.PostgresAWSCloudRuntimeDriftEvidenceLoader{
			DB: database,
			ConfigResolver: tfstatebackend.NewResolver(
				postgres.PostgresTerraformBackendQuery{DB: database},
			),
			Tracer:      tracer,
			Logger:      logger,
			Instruments: instruments,
		},
		AWSCloudRuntimeDriftWriter:           reducer.PostgresAWSCloudRuntimeDriftWriter{DB: database},
		AWSCloudRuntimeDriftLogger:           logger,
		MultiCloudRuntimeDriftEvidenceLoader: multiCloudRuntimeDriftEvidenceLoader,
		MultiCloudRuntimeDriftWriter:         multiCloudRuntimeDriftWriter,
		MultiCloudRuntimeDriftLogger:         multiCloudRuntimeDriftLogger,
	}
}

// buildReducerSearchDocumentHandlers assembles the curated search-document
// projection adapters (design 430): load the scope's current indexed content and
// write derived EshuSearchDocument facts for the search lane. No graph write.
func buildReducerSearchDocumentHandlers(
	database postgres.ExecQueryer,
	instruments *telemetry.Instruments,
	tracer trace.Tracer,
	logger *slog.Logger,
) reducer.SearchDocumentHandlers {
	return reducer.SearchDocumentHandlers{
		EshuSearchDocumentSourceLoader: postgres.NewEshuSearchDocumentSourceLoader(database),
		EshuSearchDocumentWriter: reducer.PostgresEshuSearchDocumentWriter{
			DB:              database,
			Instruments:     instruments,
			Tracer:          tracer,
			ProjectionState: postgres.NewEshuSearchDocumentProjectionStateStore(database),
		},
		EshuSearchDocumentLogger: logger,
	}
}

// buildReducerCloudInventoryHandlers assembles the cloud-inventory admission
// adapters. The admission wiring is constructed here because its six loaders
// feed only this group.
func buildReducerCloudInventoryHandlers(
	database postgres.ExecQueryer,
	logger *slog.Logger,
) reducer.CloudInventoryHandlers {
	evidenceLoader, admissionWriter, generationCheck, tagEvidenceLoader, identityPolicyEvidenceLoader, resourceChangeEvidenceLoader := cloudInventoryAdmissionWiring(database, logger)
	return reducer.CloudInventoryHandlers{
		CloudInventoryEvidenceLoader:               evidenceLoader,
		CloudInventoryAdmissionWriter:              admissionWriter,
		CloudInventoryGenerationCheck:              generationCheck,
		CloudInventoryTagEvidenceLoader:            tagEvidenceLoader,
		CloudInventoryIdentityPolicyEvidenceLoader: identityPolicyEvidenceLoader,
		CloudInventoryResourceChangeEvidenceLoader: resourceChangeEvidenceLoader,
	}
}

// buildReducerKubernetesHandlers assembles the kubernetes correlation writer and
// the canonical workload/correlation graph writers.
func buildReducerKubernetesHandlers(
	database postgres.ExecQueryer,
	graphWriters canonicalGraphWriters,
) reducer.KubernetesHandlers {
	return reducer.KubernetesHandlers{
		KubernetesCorrelationWriter: reducer.PostgresKubernetesCorrelationWriter{
			DB: database,
		},
		KubernetesWorkloadNodeWriter:    graphWriters.kubernetesWorkloadNode,
		KubernetesNamespaceNodeWriter:   graphWriters.kubernetesNamespaceNode,
		KubernetesCorrelationEdgeWriter: graphWriters.kubernetesCorrelationEdge,
	}
}

// buildReducerCrossplaneHandlers assembles the Crossplane Claim -> XRD
// SATISFIED_BY edge writer (issue #5347).
func buildReducerCrossplaneHandlers(
	graphWriters canonicalGraphWriters,
) reducer.CrossplaneHandlers {
	return reducer.CrossplaneHandlers{
		CrossplaneSatisfiedByEdgeWriter: graphWriters.crossplaneSatisfiedByEdge,
	}
}

// buildReducerSupplyChainSecurityHandlers assembles the SBOM attestation,
// supply-chain impact, security-alert reconciliation, and secrets-IAM trust/graph
// adapters, plus the endpoint-presence writers/lookups they share.
func buildReducerSupplyChainSecurityHandlers(
	database postgres.ExecQueryer,
	factStore postgres.FactStore,
	secretsIAMGraphWriter reducer.SecretsIAMGraphWriter,
	presence endpointPresenceWirings,
) reducer.SupplyChainSecurityHandlers {
	return reducer.SupplyChainSecurityHandlers{
		SBOMAttestationAttachmentWriter: reducer.PostgresSBOMAttestationAttachmentWriter{
			DB: database,
		},
		SupplyChainImpactWriter: reducer.PostgresSupplyChainImpactWriter{
			DB: database,
		},
		SecurityAlertReconciliationWriter: reducer.PostgresSecurityAlertReconciliationWriter{
			DB: database,
		},
		SecretsIAMTrustChainEvidenceLoader: factStore,
		SecretsIAMTrustChainWriter: reducer.PostgresSecretsIAMTrustChainWriter{
			DB: database,
		},
		SecretsIAMGraphWriter:             secretsIAMGraphWriter,
		EndpointPresenceWriter:            presence.secretsIAMWriter,
		EndpointPresenceLookup:            presence.secretsIAMLookup,
		APIEndpointRepoPathPresenceWriter: presence.handlesRouteWriter,
		APIEndpointRepoPathPresenceLookup: presence.handlesRouteLookup,
	}
}

// buildReducerIncidentRoutingHandlers assembles the incident-routing evidence
// adapters and the durable incident -> repository correlation wiring (#2161),
// which is constructed here because it feeds only this group.
func buildReducerIncidentRoutingHandlers(
	database postgres.ExecQueryer,
	factStore postgres.FactStore,
	graphWriters canonicalGraphWriters,
) reducer.IncidentRoutingHandlers {
	incidentRepoCorrelationLoader, incidentRepoCorrelationResolver, incidentRepoCorrelationWriter := incidentRepositoryCorrelationWiring(database)
	return reducer.IncidentRoutingHandlers{
		IncidentRoutingEvidenceLoader:        factStore,
		IncidentRoutingEvidenceWriter:        graphWriters.incidentRoutingEvidence,
		AppliedPagerDutyServiceRoutingLoader: incidentRepoCorrelationLoader,
		BackendRepositoryResolver:            incidentRepoCorrelationResolver,
		IncidentRepositoryCorrelationWriter:  incidentRepoCorrelationWriter,
	}
}

// buildReducerCodeEvidenceHandlers assembles the code taint / interprocedural /
// function-summary evidence adapters. The function-summary, source, and graph-ID
// stores plus the value-flow fixpoint projector are constructed here because they
// feed only this group.
func buildReducerCodeEvidenceHandlers(
	database postgres.ExecQueryer,
	factStore postgres.FactStore,
	graphWriters canonicalGraphWriters,
	graphReader query.GraphQuery,
	logger *slog.Logger,
) reducer.CodeEvidenceHandlers {
	functionSummaryStore := postgres.NewFunctionSummaryStore(database)
	functionSourceStore := postgres.NewFunctionSourceStore(database)
	functionGraphIDStore := postgres.NewFunctionGraphIDStore(database)
	valueFlowFixpointComponentStore := postgres.NewValueFlowFixpointComponentStore(database)
	codeInterprocLedger := postgres.NewCodeInterprocProjectedEdgeStore(database)
	codeTaintLedger := postgres.NewCodeTaintEvidenceProjectedNodeStore(database)
	valueFlowFixpointProjector := newValueFlowFixpointProjector(
		functionSummaryStore,
		functionSourceStore,
		functionGraphIDStore,
		valueFlowFixpointComponentStore,
		graphReader,
		graphWriters.codeInterprocEvidence,
		logger,
	)
	valueFlowFixpointProjector.Ledger = codeInterprocLedger
	return reducer.CodeEvidenceHandlers{
		CodeTaintEvidenceLoader:              factStore,
		CodeTaintEvidenceWriter:              graphWriters.codeTaintEvidence,
		CodeInterprocEvidenceLoader:          factStore,
		CodeInterprocEvidenceWriter:          graphWriters.codeInterprocEvidence,
		CodeFunctionSummaryLoader:            factStore,
		CodeFunctionSummaryWriter:            functionSummaryStore,
		CodeFunctionSourceLoader:             factStore,
		CodeFunctionSourceWriter:             functionSourceStore,
		CodeFunctionGraphIDLoader:            factStore,
		CodeFunctionGraphIDWriter:            functionGraphIDStore,
		ValueFlowFixpointProjector:           valueFlowFixpointProjector,
		CodeInterprocProjectedEdgeLedger:     codeInterprocLedger,
		CodeTaintEvidenceProjectedNodeLedger: codeTaintLedger,
	}
}
