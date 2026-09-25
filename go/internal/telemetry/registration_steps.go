// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract/observability"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract/thirdparty"
)

// This file continues the registrationSteps functions declared in
// registration.go (split here only because the full set of 23 steps exceeds
// the repository's 500-line file cap). See registration.go for the ordering
// contract these functions must preserve.

// registerScannerWorker inserts the scanner-worker dimensions after
// MetricDimensionCollectorKind and the scanner-worker claim/analyze/fact-batch
// spans immediately before SpanTerraformStateClaimProcess.
func registerScannerWorker() {
	metricDimensionsInserted := false
	for idx, key := range metricDimensionKeys {
		if key == MetricDimensionCollectorKind {
			metricDimensionKeys = slices.Insert(
				metricDimensionKeys,
				idx+1,
				contract.MetricDimensionAnalyzer,
				contract.MetricDimensionTargetKind,
				contract.MetricDimensionLimitKind,
			)
			metricDimensionsInserted = true
			break
		}
	}
	if !metricDimensionsInserted {
		metricDimensionKeys = append(
			metricDimensionKeys,
			contract.MetricDimensionAnalyzer,
			contract.MetricDimensionTargetKind,
			contract.MetricDimensionLimitKind,
		)
	}

	for idx, name := range spanNames {
		if name == SpanTerraformStateClaimProcess {
			spanNames = slices.Insert(
				spanNames,
				idx,
				contract.SpanScannerWorkerClaimProcess,
				contract.SpanScannerWorkerAnalyze,
				contract.SpanScannerWorkerFactEmitBatch,
			)
			return
		}
	}
	spanNames = append(
		spanNames,
		contract.SpanScannerWorkerClaimProcess,
		contract.SpanScannerWorkerAnalyze,
		contract.SpanScannerWorkerFactEmitBatch,
	)
}

// registerSemanticExtraction inserts the semantic-extraction dimensions after
// their anchors, the queue spans after SpanReducerBatchClaim, and appends the
// semantic-extraction log keys.
func registerSemanticExtraction() {
	insertMetricDimensionsAfter(
		MetricDimensionSource,
		contract.MetricDimensionSourceClass,
	)
	insertMetricDimensionsAfter(
		MetricDimensionProvider,
		contract.MetricDimensionProviderKind,
		contract.MetricDimensionProviderProfileClass,
	)
	insertMetricDimensionsAfter(
		MetricDimensionPrincipalKind,
		contract.MetricDimensionBudgetState,
		contract.MetricDimensionBudgetReason,
	)
	insertSpanNamesAfter(
		SpanReducerBatchClaim,
		contract.SpanSemanticExtractionQueueApply,
		contract.SpanSemanticExtractionQueueClaim,
		contract.SpanSemanticExtractionQueueComplete,
	)
	logKeys = append(
		logKeys,
		contract.LogKeySemanticExtractionStatus,
		contract.LogKeySemanticExtractionSourceClass,
		contract.LogKeySemanticExtractionProviderKind,
		contract.LogKeySemanticExtractionProviderProfileClass,
		contract.LogKeySemanticExtractionBudgetState,
		contract.LogKeySemanticExtractionBudgetReason,
	)
}

// insertMetricDimensionsAfter inserts values into metricDimensionKeys right
// after anchor, or appends them when anchor is not present.
func insertMetricDimensionsAfter(anchor string, values ...string) {
	for idx, key := range metricDimensionKeys {
		if key == anchor {
			metricDimensionKeys = slices.Insert(metricDimensionKeys, idx+1, values...)
			return
		}
	}
	metricDimensionKeys = append(metricDimensionKeys, values...)
}

// insertSpanNamesAfter inserts values into spanNames right after anchor, or
// appends them when anchor is not present.
func insertSpanNamesAfter(anchor string, values ...string) {
	for idx, name := range spanNames {
		if name == anchor {
			spanNames = slices.Insert(spanNames, idx+1, values...)
			return
		}
	}
	spanNames = append(spanNames, values...)
}

// registerServiceCatalog inserts the service-catalog correlation span right
// after SpanQueryCICDRunCorrelations, landing ahead of the Kubernetes
// correlation span registerKubernetes inserted at the same anchor earlier.
func registerServiceCatalog() {
	for idx, name := range spanNames {
		if name == contract.SpanQueryCICDRunCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryServiceCatalogCorrelations)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryServiceCatalogCorrelations)
}

// registerSourceTool appends the source_tool metric dimension.
func registerSourceTool() {
	metricDimensionKeys = append(metricDimensionKeys, contract.MetricDimensionSourceTool)
}

// registerSupplyChain inserts the supply-chain/vulnerability query spans
// after whichever of the Kubernetes, service-catalog, or CI/CD correlation
// spans is present, in that preference order.
func registerSupplyChain() {
	supplyChainSpans := []string{
		contract.SpanQueryContainerImageIdentities,
		contract.SpanQuerySupplyChainSecurityAlerts,
		contract.SpanQuerySBOMAttestationAttachments,
		contract.SpanQueryAdvisoryEvidence,
		contract.SpanQueryAdvisoryCatalog,
		contract.SpanQuerySupplyChainImpactFindings,
		contract.SpanQueryVulnerabilitySuppressionMutation,
		contract.SpanQuerySupplyChainImpactExplanation,
		contract.SpanQuerySupplyChainImpactAggregate,
		contract.SpanQuerySecurityAlertReconciliationAggregate,
		contract.SpanQueryContainerImageIdentityAggregate,
		contract.SpanQuerySBOMAttestationAttachmentAggregate,
	}
	for idx, name := range spanNames {
		if name == contract.SpanQueryKubernetesCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, supplyChainSpans...)
			return
		}
	}
	for idx, name := range spanNames {
		if name == contract.SpanQueryServiceCatalogCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, supplyChainSpans...)
			return
		}
	}
	for idx, name := range spanNames {
		if name == contract.SpanQueryCICDRunCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, supplyChainSpans...)
			return
		}
	}
	spanNames = append(spanNames, supplyChainSpans...)
}

// registerVaultLive appends the Vault redaction field_class metric dimension.
func registerVaultLive() {
	metricDimensionKeys = append(metricDimensionKeys, thirdparty.MetricDimensionFieldClass)
}

// registerVulnerabilityIntelligence inserts the vulnerability-intelligence
// and every hosted observability/incident source-collector span pair right
// before SpanQuerySupplyChainImpactFindings.
func registerVulnerabilityIntelligence() {
	sourceCollectorSpans := []string{
		contract.SpanVulnerabilityIntelligenceObserve,
		contract.SpanVulnerabilityIntelligenceFetch,
		contract.SpanSecurityAlertObserve,
		contract.SpanSecurityAlertFetch,
		thirdparty.SpanPagerDutyObserve,
		thirdparty.SpanPagerDutyFetch,
		thirdparty.SpanJiraObserve,
		thirdparty.SpanJiraFetch,
		observability.SpanGrafanaObserve,
		observability.SpanGrafanaFetch,
		observability.SpanPrometheusMimirObserve,
		observability.SpanPrometheusMimirFetch,
		observability.SpanLokiObserve,
		observability.SpanLokiFetch,
		observability.SpanTempoObserve,
		observability.SpanTempoFetch,
	}
	for idx, name := range spanNames {
		if name == contract.SpanQuerySupplyChainImpactFindings {
			spanNames = slices.Insert(spanNames, idx, sourceCollectorSpans...)
			return
		}
	}
	spanNames = append(spanNames, sourceCollectorSpans...)
}

// registerIncidentContext inserts the incident-context query span after
// SpanQueryAdvisoryEvidence.
func registerIncidentContext() {
	for idx, name := range spanNames {
		if name == contract.SpanQueryAdvisoryEvidence {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryIncidentContext)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryIncidentContext)
}

// registerObservabilityCoverage inserts the observability-coverage
// correlation span after the Kubernetes correlation span when present,
// otherwise after the service-catalog correlation span. Keeps the frozen
// read-model span order stable as the query surface grows.
func registerObservabilityCoverage() {
	for idx, name := range spanNames {
		if name == contract.SpanQueryKubernetesCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryObservabilityCoverageCorrelations)
			return
		}
	}
	for idx, name := range spanNames {
		if name == contract.SpanQueryServiceCatalogCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryObservabilityCoverageCorrelations)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryObservabilityCoverageCorrelations)
}

// registerSecretsIAM inserts the frozen-order set of secrets/IAM query spans
// (identity trust-chain span first, summary rollup last) after the
// observability-coverage correlation span when present, otherwise after the
// Kubernetes correlation span.
func registerSecretsIAM() {
	secretsIAMQuerySpans := []string{
		contract.SpanQuerySecretsIAMIdentityTrustChains,
		contract.SpanQuerySecretsIAMPrivilegePostureObservations,
		contract.SpanQuerySecretsIAMSecretAccessPaths,
		contract.SpanQuerySecretsIAMPostureGaps,
		contract.SpanQuerySecretsIAMPostureSummary,
	}
	for idx, name := range spanNames {
		if name == contract.SpanQueryObservabilityCoverageCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, secretsIAMQuerySpans...)
			return
		}
	}
	for idx, name := range spanNames {
		if name == contract.SpanQueryKubernetesCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, secretsIAMQuerySpans...)
			return
		}
	}
	spanNames = append(spanNames, secretsIAMQuerySpans...)
}

// registerWorkItemEvidence inserts the work-item evidence query span after
// SpanQueryIncidentContext.
func registerWorkItemEvidence() {
	for idx, name := range spanNames {
		if name == contract.SpanQueryIncidentContext {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryWorkItemEvidence)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryWorkItemEvidence)
}

// registerGenerationLifecycle inserts the generation-lifecycle drilldown
// query span after SpanQueryWorkItemEvidence.
func registerGenerationLifecycle() {
	for idx, name := range spanNames {
		if name == contract.SpanQueryWorkItemEvidence {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryFreshnessGenerationLifecycle)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryFreshnessGenerationLifecycle)
}

// registerChangedSince inserts the repository-scope changed-since query span
// after SpanQueryFreshnessGenerationLifecycle.
func registerChangedSince() {
	for idx, name := range spanNames {
		if name == contract.SpanQueryFreshnessGenerationLifecycle {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryFreshnessChangedSince)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryFreshnessChangedSince)
}

// registerServiceChangedSince inserts the service-scope changed-since query
// span after SpanQueryFreshnessChangedSince.
func registerServiceChangedSince() {
	for idx, name := range spanNames {
		if name == contract.SpanQueryFreshnessChangedSince {
			spanNames = slices.Insert(spanNames, idx+1, contract.SpanQueryFreshnessServiceChangedSince)
			return
		}
	}
	spanNames = append(spanNames, contract.SpanQueryFreshnessServiceChangedSince)
}

// registerProducerGrant appends the producer-grant decision log keys (#6726).
// It adds no metric dimension keys (decision, stage, reason, and fact_kind are
// already registered) and no span names (the decision is a span event on the
// active span, not a new span), so it only extends logKeys, unconditionally at
// the end and independent of every earlier step.
func registerProducerGrant() {
	logKeys = append(
		logKeys,
		contract.LogKeyProducerGrantProducerID,
		contract.LogKeyProducerGrantVersion,
		contract.LogKeyProducerGrantDecision,
		contract.LogKeyProducerGrantStage,
		contract.LogKeyProducerGrantReason,
		contract.LogKeyProducerGrantFactKind,
	)
}
