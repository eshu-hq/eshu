// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// registrationSteps is the explicit, ordered list of per-family telemetry
// contract registrations that used to run as 23 independent filename-sorted
// func init() bodies across contract_*.go (issue #6777). Each step below is
// one of those init bodies, copied verbatim and renamed, still mutating the
// package-level spanNames, metricDimensionKeys, and logKeys slices declared in
// registry.go.
//
// THIS ORDER IS LOAD-BEARING. Every step below does one of two things:
//
//   - append unconditionally (no dependency on prior steps), or
//   - anchor-insert: scan the current spanNames/metricDimensionKeys slice for
//     a specific existing entry and slices.Insert its own entries immediately
//     after it, falling back to append when the anchor is absent.
//
// Several anchor-insert steps anchor on a span or dimension that an EARLIER
// step in this exact slice just inserted (for example registerSupplyChain
// anchors on SpanQueryKubernetesCorrelations, which registerKubernetes
// inserted three steps earlier). Reordering this slice changes which anchor
// each step finds and therefore changes the frozen output order captured
// verbatim by TestSpanNames, TestMetricDimensionKeys, and TestLogKeys in
// contract_test.go. The step order below reproduces the original filename
// sort order (admission_decisions, bootstrap_ingestion, cicd, collector_run,
// collector_stage, kubernetes, language_query, package_registry,
// s3_external_principal, scanner_worker, semantic_extraction, service_catalog,
// source_tool, supply_chain, vaultlive, vulnerability_intelligence,
// z_incident_context, z_observability_coverage, z_secrets_iam,
// zz_work_item_evidence, zzz_generation_lifecycle, zzzz_changed_since,
// zzzz_service_changed_since) exactly, then appends producer_grant (#6726)
// last; do not resort it alphabetically by Go function name or reflow it
// without re-running the order tests.
var registrationSteps = []func(){
	registerAdmissionDecisions,
	registerBootstrapIngestion,
	registerCICD,
	registerCollectorRun,
	registerCollectorStage,
	registerKubernetes,
	registerLanguageQuery,
	registerPackageRegistry,
	registerS3ExternalPrincipal,
	registerScannerWorker,
	registerSemanticExtraction,
	registerServiceCatalog,
	registerSourceTool,
	registerSupplyChain,
	registerVaultLive,
	registerVulnerabilityIntelligence,
	registerIncidentContext,
	registerObservabilityCoverage,
	registerSecretsIAM,
	registerWorkItemEvidence,
	registerGenerationLifecycle,
	registerChangedSince,
	registerServiceChangedSince,
	registerProducerGrant,
}

func init() {
	for _, step := range registrationSteps {
		step()
	}
}

// registerAdmissionDecisions inserts the admission-decisions query span
// immediately after SpanQueryRelationshipEvidence.
func registerAdmissionDecisions() {
	for idx, name := range spanNames {
		if name == SpanQueryRelationshipEvidence {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryAdmissionDecisions)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryAdmissionDecisions)
}

// registerBootstrapIngestion appends the bootstrap pipeline stage dimensions
// and the bootstrap collector-cycle span. MetricDimensionSourceFileKind and
// MetricDimensionBootstrapPhase stay declared in root contract.go.
func registerBootstrapIngestion() {
	metricDimensionKeys = append(
		metricDimensionKeys,
		MetricDimensionSourceFileKind,
		MetricDimensionBootstrapPhase,
	)
	spanNames = append(spanNames, contract.SpanBootstrapCollectorCycle)
}

// registerCICD inserts the CI/CD run correlation and correlation-aggregate
// query spans after package-registry dependencies (or correlations, once
// registerPackageRegistry has run later — the second anchor loop exists for
// resilience but the first anchor, SpanQueryPackageRegistryCorrelations, is
// not yet present when this step runs, so it falls through to the
// SpanQueryPackageRegistryDependencies anchor in practice), then places the
// CI/CD source-collector spans ahead of the AWS collector claim-process span.
func registerCICD() {
	for idx, name := range spanNames {
		if name == contract.SpanQueryPackageRegistryCorrelations {
			spanNames = slices.Insert(
				spanNames, idx+1,
				contract.SpanQueryCICDRunCorrelations,
				contract.SpanQueryCICDRunCorrelationAggregate,
			)
			insertCICDRunSourceSpans()
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryDependencies {
			spanNames = slices.Insert(
				spanNames, idx+1,
				contract.SpanQueryCICDRunCorrelations,
				contract.SpanQueryCICDRunCorrelationAggregate,
			)
			insertCICDRunSourceSpans()
			return
		}
	}
	spanNames = append(
		spanNames,
		contract.SpanQueryCICDRunCorrelations,
		contract.SpanQueryCICDRunCorrelationAggregate,
	)
	insertCICDRunSourceSpans()
}

func insertCICDRunSourceSpans() {
	if slices.Contains(spanNames, contract.SpanCICDRunObserve) {
		return
	}
	for idx, name := range spanNames {
		if name == SpanAWSCollectorClaimProcess {
			spanNames = slices.Insert(
				spanNames, idx,
				contract.SpanCICDRunObserve,
				contract.SpanCICDRunFetch,
			)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanCICDRunObserve, contract.SpanCICDRunFetch)
}

// registerCollectorRun appends the claimed-service run span. The outcome
// label it carries uses the existing MetricDimensionOutcome ("outcome") key
// already in the root registry, so no new dimension key is registered here.
func registerCollectorRun() {
	spanNames = append(spanNames, contract.SpanCollectorClaimedRun)
}

// registerCollectorStage appends the git-collector snapshot stage dimension
// and span.
func registerCollectorStage() {
	metricDimensionKeys = append(metricDimensionKeys, contract.MetricDimensionStage)
	spanNames = append(spanNames, contract.SpanCollectorSnapshotStage)
}

// registerKubernetes inserts the Kubernetes correlation query span right
// after SpanQueryCICDRunCorrelations. This step runs before
// registerServiceCatalog (which appears later in registrationSteps and
// anchors on the same span), so registerServiceCatalog's insert lands ahead
// of this one, yielding the frozen order ci_cd_run_correlations,
// service_catalog_correlations, kubernetes_correlations.
func registerKubernetes() {
	for idx, name := range spanNames {
		if name == contract.SpanQueryCICDRunCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryKubernetesCorrelations)
			return
		}
	}
	for idx, name := range spanNames {
		if name == contract.SpanQueryServiceCatalogCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryKubernetesCorrelations)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryKubernetesCorrelations)
}

// registerLanguageQuery appends the language-query span after
// SpanQueryCodeownersOwnership.
func registerLanguageQuery() {
	for idx, name := range spanNames {
		if name == SpanQueryCodeownersOwnership {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryLanguageQuery)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryLanguageQuery)
}

// registerPackageRegistry inserts the package-registry correlation,
// dependency-chain, and aggregate spans after
// SpanQueryPackageRegistryDependencies.
func registerPackageRegistry() {
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryDependencies {
			spanNames = slices.Insert(
				spanNames, idx+1,
				contract.SpanQueryPackageRegistryCorrelations,
				contract.SpanQueryPackageRegistryDependencyChains,
				contract.SpanQueryPackageRegistryAggregate,
			)
			return
		}
	}
	spanNames = append(
		spanNames,
		contract.SpanQueryPackageRegistryCorrelations,
		contract.SpanQueryPackageRegistryDependencyChains,
		contract.SpanQueryPackageRegistryAggregate,
	)
}

// registerS3ExternalPrincipal inserts the S3 external-principal grant
// materialization span after SpanReducerS3LogsToMaterialization.
func registerS3ExternalPrincipal() {
	for idx, name := range spanNames {
		if name == SpanReducerS3LogsToMaterialization {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanReducerS3ExternalPrincipalGrantMaterialization)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanReducerS3ExternalPrincipalGrantMaterialization)
}
