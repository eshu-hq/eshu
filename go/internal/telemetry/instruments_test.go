// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry //nolint:filelength // 814-line test table for all Attr* helpers and instrument registrations; splitting would break the per-helper coverage table invariant.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewInstrumentsNoError(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	inst, err := NewInstruments(meter)

	require.NoError(t, err, "NewInstruments should succeed with noop meter")
	require.NotNil(t, inst, "Instruments should not be nil")

	// Verify all counter fields are non-nil
	assert.NotNil(t, inst.FactsEmitted, "FactsEmitted counter should be registered")
	assert.NotNil(t, inst.FactsCommitted, "FactsCommitted counter should be registered")
	assert.NotNil(t, inst.ProjectionsCompleted, "ProjectionsCompleted counter should be registered")
	assert.NotNil(t, inst.ReducerIntentsEnqueued, "ReducerIntentsEnqueued counter should be registered")
	assert.NotNil(t, inst.ReducerAdmissionDeferrals, "ReducerAdmissionDeferrals counter should be registered")
	assert.NotNil(t, inst.ReducerExecutions, "ReducerExecutions counter should be registered")
	assert.NotNil(t, inst.CanonicalWrites, "CanonicalWrites counter should be registered")
	assert.NotNil(t, inst.SharedProjectionCycles, "SharedProjectionCycles counter should be registered")
	assert.NotNil(t, inst.SharedAcceptanceUpserts, "SharedAcceptanceUpserts counter should be registered")
	assert.NotNil(t, inst.SharedAcceptanceLookupErrors, "SharedAcceptanceLookupErrors counter should be registered")
	assert.NotNil(t, inst.SharedProjectionStaleIntents, "SharedProjectionStaleIntents counter should be registered")
	assert.NotNil(t, inst.GenerationRetentionPruned, "GenerationRetentionPruned counter should be registered")
	assert.NotNil(t, inst.GenerationRetentionRowsPruned, "GenerationRetentionRowsPruned counter should be registered")
	assert.NotNil(t, inst.GenerationRetentionFailures, "GenerationRetentionFailures counter should be registered")
	assert.NotNil(t, inst.GenerationRetentionSkipped, "GenerationRetentionSkipped counter should be registered")
	assert.NotNil(t, inst.DeltaBaselineFallbacks, "DeltaBaselineFallbacks counter should be registered")
	assert.NotNil(t, inst.ReconciliationFullSnapshots, "ReconciliationFullSnapshots counter should be registered")
	assert.NotNil(t, inst.ReconciliationDriftRetractions, "ReconciliationDriftRetractions counter should be registered")
	assert.NotNil(t, inst.ReconciliationConvergence, "ReconciliationConvergence counter should be registered")
	assert.NotNil(t, inst.DocumentationEntityMentions, "DocumentationEntityMentions counter should be registered")
	assert.NotNil(t, inst.DocumentationClaimCandidates, "DocumentationClaimCandidates counter should be registered")
	assert.NotNil(t, inst.DocumentationClaimsSuppressed, "DocumentationClaimsSuppressed counter should be registered")
	assert.NotNil(t, inst.DocumentationDriftFindings, "DocumentationDriftFindings counter should be registered")
	assert.NotNil(t, inst.IaCReachabilityRows, "IaCReachabilityRows counter should be registered")
	assert.NotNil(t, inst.TerraformStateSnapshotsObserved, "TerraformStateSnapshotsObserved counter should be registered")
	assert.NotNil(t, inst.TerraformStateResourcesEmitted, "TerraformStateResourcesEmitted counter should be registered")
	assert.NotNil(t, inst.TerraformStateOutputsEmitted, "TerraformStateOutputsEmitted counter should be registered")
	assert.NotNil(t, inst.TerraformStateModulesEmitted, "TerraformStateModulesEmitted counter should be registered")
	assert.NotNil(t, inst.TerraformStateWarningsEmitted, "TerraformStateWarningsEmitted counter should be registered")
	assert.NotNil(t, inst.TerraformStateRedactionsApplied, "TerraformStateRedactionsApplied counter should be registered")
	assert.NotNil(t, inst.TerraformStateS3ConditionalGetNotModified, "TerraformStateS3ConditionalGetNotModified counter should be registered")
	assert.NotNil(t, inst.TerraformStateDiscoveryCandidates, "TerraformStateDiscoveryCandidates counter should be registered")
	assert.NotNil(t, inst.SecretsIAMSourceRedactions, "SecretsIAMSourceRedactions counter should be registered")
	assert.NotNil(t, inst.SecretsIAMSourceScopeFreshness, "SecretsIAMSourceScopeFreshness gauge should be registered")
	assert.NotNil(t, inst.PackageRegistryRequests, "PackageRegistryRequests counter should be registered")
	assert.NotNil(t, inst.PackageRegistryFactsEmitted, "PackageRegistryFactsEmitted counter should be registered")
	assert.NotNil(t, inst.PackageRegistryRateLimited, "PackageRegistryRateLimited counter should be registered")
	assert.NotNil(t, inst.PackageRegistryParseFailures, "PackageRegistryParseFailures counter should be registered")
	assert.NotNil(t, inst.VulnerabilityIntelligenceObservations, "VulnerabilityIntelligenceObservations counter should be registered")
	assert.NotNil(t, inst.VulnerabilityIntelligenceFactsEmitted, "VulnerabilityIntelligenceFactsEmitted counter should be registered")
	assert.NotNil(t, inst.VulnerabilityIntelligenceRateLimited, "VulnerabilityIntelligenceRateLimited counter should be registered")
	assert.NotNil(t, inst.SecurityAlertProviderRequests, "SecurityAlertProviderRequests counter should be registered")
	assert.NotNil(t, inst.SecurityAlertFactsEmitted, "SecurityAlertFactsEmitted counter should be registered")
	assert.NotNil(t, inst.SecurityAlertRateLimited, "SecurityAlertRateLimited counter should be registered")
	assert.NotNil(t, inst.CICDRunProviderRequests, "CICDRunProviderRequests counter should be registered")
	assert.NotNil(t, inst.CICDRunFactsEmitted, "CICDRunFactsEmitted counter should be registered")
	assert.NotNil(t, inst.CICDRunRateLimited, "CICDRunRateLimited counter should be registered")
	assert.NotNil(t, inst.CICDRunPartialGenerations, "CICDRunPartialGenerations counter should be registered")
	assert.NotNil(t, inst.PagerDutyProviderRequests, "PagerDutyProviderRequests counter should be registered")
	assert.NotNil(t, inst.PagerDutyFactsEmitted, "PagerDutyFactsEmitted counter should be registered")
	assert.NotNil(t, inst.PagerDutyRateLimited, "PagerDutyRateLimited counter should be registered")
	assert.NotNil(t, inst.PagerDutyConfigResourcesObserved, "PagerDutyConfigResourcesObserved counter should be registered")
	assert.NotNil(t, inst.PagerDutyConfigDriftCandidates, "PagerDutyConfigDriftCandidates counter should be registered")
	assert.NotNil(t, inst.PagerDutyConfigPartialFailures, "PagerDutyConfigPartialFailures counter should be registered")
	assert.NotNil(t, inst.PagerDutyConfigRedactions, "PagerDutyConfigRedactions counter should be registered")
	assert.NotNil(t, inst.JiraProviderRequests, "JiraProviderRequests counter should be registered")
	assert.NotNil(t, inst.JiraFactsEmitted, "JiraFactsEmitted counter should be registered")
	assert.NotNil(t, inst.JiraRateLimited, "JiraRateLimited counter should be registered")
	assert.NotNil(t, inst.GrafanaProviderRequests, "GrafanaProviderRequests counter should be registered")
	assert.NotNil(t, inst.GrafanaFactsEmitted, "GrafanaFactsEmitted counter should be registered")
	assert.NotNil(t, inst.GrafanaRateLimited, "GrafanaRateLimited counter should be registered")
	assert.NotNil(t, inst.GrafanaRetries, "GrafanaRetries counter should be registered")
	assert.NotNil(t, inst.GrafanaRedactions, "GrafanaRedactions counter should be registered")
	assert.NotNil(t, inst.PrometheusMimirProviderRequests, "PrometheusMimirProviderRequests counter should be registered")
	assert.NotNil(t, inst.PrometheusMimirFactsEmitted, "PrometheusMimirFactsEmitted counter should be registered")
	assert.NotNil(t, inst.PrometheusMimirRateLimited, "PrometheusMimirRateLimited counter should be registered")
	assert.NotNil(t, inst.PrometheusMimirRetries, "PrometheusMimirRetries counter should be registered")
	assert.NotNil(t, inst.PrometheusMimirRedactions, "PrometheusMimirRedactions counter should be registered")
	assert.NotNil(t, inst.PrometheusMimirStale, "PrometheusMimirStale counter should be registered")
	assert.NotNil(t, inst.LokiProviderRequests, "LokiProviderRequests counter should be registered")
	assert.NotNil(t, inst.LokiFactsEmitted, "LokiFactsEmitted counter should be registered")
	assert.NotNil(t, inst.LokiRateLimited, "LokiRateLimited counter should be registered")
	assert.NotNil(t, inst.LokiRetries, "LokiRetries counter should be registered")
	assert.NotNil(t, inst.LokiRedactions, "LokiRedactions counter should be registered")
	assert.NotNil(t, inst.LokiHighCardinalityRejected, "LokiHighCardinalityRejected counter should be registered")
	assert.NotNil(t, inst.LokiStale, "LokiStale counter should be registered")
	assert.NotNil(t, inst.TempoProviderRequests, "TempoProviderRequests counter should be registered")
	assert.NotNil(t, inst.TempoFactsEmitted, "TempoFactsEmitted counter should be registered")
	assert.NotNil(t, inst.TempoRateLimited, "TempoRateLimited counter should be registered")
	assert.NotNil(t, inst.TempoRetries, "TempoRetries counter should be registered")
	assert.NotNil(t, inst.TempoRedactions, "TempoRedactions counter should be registered")
	assert.NotNil(t, inst.TempoHighCardinalityRejected, "TempoHighCardinalityRejected counter should be registered")
	assert.NotNil(t, inst.TempoStale, "TempoStale counter should be registered")
	assert.NotNil(t, inst.ScannerWorkerClaims, "ScannerWorkerClaims counter should be registered")
	assert.NotNil(t, inst.ScannerWorkerRetries, "ScannerWorkerRetries counter should be registered")
	assert.NotNil(t, inst.ScannerWorkerDeadLetters, "ScannerWorkerDeadLetters counter should be registered")
	assert.NotNil(t, inst.ScannerWorkerFactsEmitted, "ScannerWorkerFactsEmitted counter should be registered")
	assert.NotNil(t, inst.PackageSourceCorrelations, "PackageSourceCorrelations counter should be registered")
	assert.NotNil(t, inst.ContainerImageIdentityDecisions, "ContainerImageIdentityDecisions counter should be registered")
	assert.NotNil(t, inst.CICDRunCorrelations, "CICDRunCorrelations counter should be registered")
	assert.NotNil(t, inst.ServiceCatalogCorrelations, "ServiceCatalogCorrelations counter should be registered")
	assert.NotNil(t, inst.ServiceCatalogCorrelationGuardrails, "ServiceCatalogCorrelationGuardrails counter should be registered")
	assert.NotNil(t, inst.SearchDecayPolicyApplications, "SearchDecayPolicyApplications counter should be registered")
	assert.NotNil(t, inst.ObservabilityCoverageCorrelations, "ObservabilityCoverageCorrelations counter should be registered")
	assert.NotNil(t, inst.ObservabilityCoverageEdges, "ObservabilityCoverageEdges counter should be registered")
	assert.NotNil(t, inst.IAMCanPerformConditioned, "IAMCanPerformConditioned counter should be registered")
	assert.NotNil(t, inst.IncidentRoutingEvidence, "IncidentRoutingEvidence counter should be registered")
	assert.NotNil(t, inst.KubernetesCorrelations, "KubernetesCorrelations counter should be registered")
	assert.NotNil(t, inst.SBOMAttestationAttachments, "SBOMAttestationAttachments counter should be registered")
	assert.NotNil(t, inst.SupplyChainImpactFindings, "SupplyChainImpactFindings counter should be registered")
	assert.NotNil(t, inst.ConfluenceHTTPRequests, "ConfluenceHTTPRequests counter should be registered")
	assert.NotNil(t, inst.ConfluencePermissionDeniedPages, "ConfluencePermissionDeniedPages counter should be registered")
	assert.NotNil(t, inst.ConfluenceDocumentsObserved, "ConfluenceDocumentsObserved counter should be registered")
	assert.NotNil(t, inst.ConfluenceSectionsEmitted, "ConfluenceSectionsEmitted counter should be registered")
	assert.NotNil(t, inst.ConfluenceLinksEmitted, "ConfluenceLinksEmitted counter should be registered")
	assert.NotNil(t, inst.ConfluenceSyncFailures, "ConfluenceSyncFailures counter should be registered")
	assert.NotNil(t, inst.CorrelationDriftIntentsEnqueued, "CorrelationDriftIntentsEnqueued counter should be registered")
	assert.NotNil(t, inst.CorrelationOrphanDetected, "CorrelationOrphanDetected counter should be registered")
	assert.NotNil(t, inst.CorrelationUnmanagedDetected, "CorrelationUnmanagedDetected counter should be registered")
	assert.NotNil(t, inst.WebhookRequests, "WebhookRequests counter should be registered")
	assert.NotNil(t, inst.WebhookTriggerDecisions, "WebhookTriggerDecisions counter should be registered")
	assert.NotNil(t, inst.WebhookStoreOperations, "WebhookStoreOperations counter should be registered")
	assert.NotNil(t, inst.SemanticExtractionQueueEvents, "SemanticExtractionQueueEvents counter should be registered")
	assert.NotNil(t, inst.SemanticExtractionBudgetTokens, "SemanticExtractionBudgetTokens counter should be registered")
	assert.NotNil(t, inst.SemanticExtractionBudgetCostMicros, "SemanticExtractionBudgetCostMicros counter should be registered")
	assert.NotNil(t, inst.AWSAPICalls, "AWSAPICalls counter should be registered")
	assert.NotNil(t, inst.AWSThrottles, "AWSThrottles counter should be registered")
	assert.NotNil(t, inst.AWSAssumeRoleFailed, "AWSAssumeRoleFailed counter should be registered")
	assert.NotNil(t, inst.AWSBudgetExhausted, "AWSBudgetExhausted counter should be registered")
	assert.NotNil(t, inst.AWSCheckpointEvents, "AWSCheckpointEvents counter should be registered")
	assert.NotNil(t, inst.AWSResourcesEmitted, "AWSResourcesEmitted counter should be registered")
	assert.NotNil(t, inst.AWSRelationshipsEmitted, "AWSRelationshipsEmitted counter should be registered")
	assert.NotNil(t, inst.AWSTagObservationsEmitted, "AWSTagObservationsEmitted counter should be registered")
	assert.NotNil(t, inst.AWSFreshnessEvents, "AWSFreshnessEvents counter should be registered")
	assert.NotNil(t, inst.AWSOrgAccessSkipped, "AWSOrgAccessSkipped counter should be registered")
	assert.NotNil(t, inst.GCPMaterializationFacts, "GCPMaterializationFacts counter should be registered")
	assert.NotNil(t, inst.GCPMaterializationGraphWrites, "GCPMaterializationGraphWrites counter should be registered")
	assert.NotNil(t, inst.AWSScanStatusStaleFence, "AWSScanStatusStaleFence counter should be registered")
	assert.NotNil(t, inst.WorkflowClaimAttemptBudgetExhausted, "WorkflowClaimAttemptBudgetExhausted counter should be registered")

	// Verify all histogram fields are non-nil
	assert.NotNil(t, inst.CollectorObserveDuration, "CollectorObserveDuration histogram should be registered")
	assert.NotNil(t, inst.WorkflowClaimWaitDuration, "WorkflowClaimWaitDuration histogram should be registered")
	assert.NotNil(t, inst.TerraformStateClaimWaitDuration, "TerraformStateClaimWaitDuration histogram should be registered")
	assert.NotNil(t, inst.ScopeAssignDuration, "ScopeAssignDuration histogram should be registered")
	assert.NotNil(t, inst.FactEmitDuration, "FactEmitDuration histogram should be registered")
	assert.NotNil(t, inst.ProjectorRunDuration, "ProjectorRunDuration histogram should be registered")
	assert.NotNil(t, inst.ProjectorStageDuration, "ProjectorStageDuration histogram should be registered")
	assert.NotNil(t, inst.ReducerRunDuration, "ReducerRunDuration histogram should be registered")
	assert.NotNil(t, inst.GCPMaterializationDuration, "GCPMaterializationDuration histogram should be registered")
	assert.NotNil(t, inst.GenerationRetentionDuration, "GenerationRetentionDuration histogram should be registered")
	assert.NotNil(t, inst.GenerationRetentionBatchSize, "GenerationRetentionBatchSize histogram should be registered")
	assert.NotNil(t, inst.GenerationRetentionOldestEligibleAge, "GenerationRetentionOldestEligibleAge histogram should be registered")
	assert.NotNil(t, inst.CanonicalWriteDuration, "CanonicalWriteDuration histogram should be registered")
	assert.NotNil(t, inst.QueueClaimDuration, "QueueClaimDuration histogram should be registered")
	assert.NotNil(t, inst.BatchClaimSize, "BatchClaimSize histogram should be registered")
	assert.NotNil(t, inst.PostgresQueryDuration, "PostgresQueryDuration histogram should be registered")
	assert.NotNil(t, inst.Neo4jQueryDuration, "Neo4jQueryDuration histogram should be registered")
	assert.NotNil(t, inst.SharedEdgeWriteGroups, "SharedEdgeWriteGroups counter should be registered")
	assert.NotNil(t, inst.SharedEdgeWriteGroupDuration, "SharedEdgeWriteGroupDuration histogram should be registered")
	assert.NotNil(t, inst.SharedEdgeWriteGroupStatementCount, "SharedEdgeWriteGroupStatementCount histogram should be registered")
	assert.NotNil(t, inst.CodeCallEdgeBatches, "CodeCallEdgeBatches counter should be registered")
	assert.NotNil(t, inst.CodeCallEdgeDuration, "CodeCallEdgeDuration histogram should be registered")
	assert.NotNil(t, inst.SharedAcceptanceUpsertDuration, "SharedAcceptanceUpsertDuration histogram should be registered")
	assert.NotNil(t, inst.SharedAcceptanceLookupDuration, "SharedAcceptanceLookupDuration histogram should be registered")
	assert.NotNil(t, inst.SharedAcceptancePrefetchSize, "SharedAcceptancePrefetchSize histogram should be registered")
	assert.NotNil(t, inst.SharedProjectionIntentWaitDuration, "SharedProjectionIntentWaitDuration histogram should be registered")
	assert.NotNil(t, inst.SharedProjectionProcessingDuration, "SharedProjectionProcessingDuration histogram should be registered")
	assert.NotNil(t, inst.SharedProjectionStepDuration, "SharedProjectionStepDuration histogram should be registered")
	assert.NotNil(t, inst.DocumentationDriftGenerationDuration, "DocumentationDriftGenerationDuration histogram should be registered")
	assert.NotNil(t, inst.IaCReachabilityMaterializationDuration, "IaCReachabilityMaterializationDuration histogram should be registered")
	assert.NotNil(t, inst.TerraformStateSnapshotBytes, "TerraformStateSnapshotBytes histogram should be registered")
	assert.NotNil(t, inst.TerraformStateParseDuration, "TerraformStateParseDuration histogram should be registered")
	assert.NotNil(t, inst.DependencyListDuration, "DependencyListDuration histogram should be registered")
	assert.NotNil(t, inst.DependencyListErrors, "DependencyListErrors counter should be registered")
	assert.NotNil(t, inst.APIRequestDuration, "APIRequestDuration histogram should be registered")
	assert.NotNil(t, inst.APIRequestErrors, "APIRequestErrors counter should be registered")
	assert.NotNil(t, inst.OIDCLoginThrottled, "OIDCLoginThrottled counter should be registered")
	assert.NotNil(t, inst.PackageRegistryObserveDuration, "PackageRegistryObserveDuration histogram should be registered")
	assert.NotNil(t, inst.PackageRegistryGenerationLag, "PackageRegistryGenerationLag histogram should be registered")
	assert.NotNil(t, inst.VulnerabilityIntelligenceFetchDuration, "VulnerabilityIntelligenceFetchDuration histogram should be registered")
	assert.NotNil(t, inst.SecurityAlertFetchDuration, "SecurityAlertFetchDuration histogram should be registered")
	assert.NotNil(t, inst.CICDRunFetchDuration, "CICDRunFetchDuration histogram should be registered")
	assert.NotNil(t, inst.PagerDutyFetchDuration, "PagerDutyFetchDuration histogram should be registered")
	assert.NotNil(t, inst.PagerDutyGenerationLag, "PagerDutyGenerationLag histogram should be registered")
	assert.NotNil(t, inst.JiraFetchDuration, "JiraFetchDuration histogram should be registered")
	assert.NotNil(t, inst.GrafanaFetchDuration, "GrafanaFetchDuration histogram should be registered")
	assert.NotNil(t, inst.PrometheusMimirFetchDuration, "PrometheusMimirFetchDuration histogram should be registered")
	assert.NotNil(t, inst.LokiFetchDuration, "LokiFetchDuration histogram should be registered")
	assert.NotNil(t, inst.TempoFetchDuration, "TempoFetchDuration histogram should be registered")
	assert.NotNil(t, inst.ScannerWorkerQueueWaitDuration, "ScannerWorkerQueueWaitDuration histogram should be registered")
	assert.NotNil(t, inst.ScannerWorkerScanDuration, "ScannerWorkerScanDuration histogram should be registered")
	assert.NotNil(t, inst.ScannerWorkerTargetCount, "ScannerWorkerTargetCount histogram should be registered")
	assert.NotNil(t, inst.ScannerWorkerResultCount, "ScannerWorkerResultCount histogram should be registered")
	assert.NotNil(t, inst.ScannerWorkerCPUSeconds, "ScannerWorkerCPUSeconds histogram should be registered")
	assert.NotNil(t, inst.ScannerWorkerMemoryBytes, "ScannerWorkerMemoryBytes histogram should be registered")
	assert.NotNil(t, inst.ConfluenceFetchDuration, "ConfluenceFetchDuration histogram should be registered")
	assert.NotNil(t, inst.WebhookRequestDuration, "WebhookRequestDuration histogram should be registered")
	assert.NotNil(t, inst.WebhookStoreDuration, "WebhookStoreDuration histogram should be registered")
	assert.NotNil(t, inst.AWSScanDuration, "AWSScanDuration histogram should be registered")
}

func TestNewInstrumentsNilMeterError(t *testing.T) {
	inst, err := NewInstruments(nil)

	require.Error(t, err, "NewInstruments should fail with nil meter")
	assert.Nil(t, inst, "Instruments should be nil on error")
	assert.Contains(t, err.Error(), "meter is required", "Error should mention meter requirement")
}

func TestAttrHelpers(t *testing.T) {
	tests := []struct {
		name     string
		attrFunc func(string) string
		wantKey  string
	}{
		{
			name:     "AttrScopeID",
			attrFunc: func(v string) string { return string(AttrScopeID(v).Key) },
			wantKey:  LogKeyScopeID,
		},
		{
			name:     "AttrScopeKind",
			attrFunc: func(v string) string { return string(AttrScopeKind(v).Key) },
			wantKey:  MetricDimensionScopeKind,
		},
		{
			name:     "AttrSource",
			attrFunc: func(v string) string { return string(AttrSource(v).Key) },
			wantKey:  MetricDimensionSource,
		},
		{
			name:     "AttrSourceClass",
			attrFunc: func(v string) string { return string(AttrSourceClass(v).Key) },
			wantKey:  MetricDimensionSourceClass,
		},
		{
			name:     "AttrSourceSystem",
			attrFunc: func(v string) string { return string(AttrSourceSystem(v).Key) },
			wantKey:  MetricDimensionSourceSystem,
		},
		{
			name:     "AttrGenerationID",
			attrFunc: func(v string) string { return string(AttrGenerationID(v).Key) },
			wantKey:  MetricDimensionGenerationID,
		},
		{
			name:     "AttrCollectorKind",
			attrFunc: func(v string) string { return string(AttrCollectorKind(v).Key) },
			wantKey:  MetricDimensionCollectorKind,
		},
		{
			name:     "AttrAnalyzer",
			attrFunc: func(v string) string { return string(AttrAnalyzer(v).Key) },
			wantKey:  MetricDimensionAnalyzer,
		},
		{
			name:     "AttrTargetKind",
			attrFunc: func(v string) string { return string(AttrTargetKind(v).Key) },
			wantKey:  MetricDimensionTargetKind,
		},
		{
			name:     "AttrLimitKind",
			attrFunc: func(v string) string { return string(AttrLimitKind(v).Key) },
			wantKey:  MetricDimensionLimitKind,
		},
		{
			name:     "AttrDomain",
			attrFunc: func(v string) string { return string(AttrDomain(v).Key) },
			wantKey:  MetricDimensionDomain,
		},
		{
			name:     "AttrPartitionKey",
			attrFunc: func(v string) string { return string(AttrPartitionKey(v).Key) },
			wantKey:  MetricDimensionPartitionKey,
		},
		{
			name:     "AttrRunner",
			attrFunc: func(v string) string { return string(AttrRunner(v).Key) },
			wantKey:  MetricDimensionRunner,
		},
		{
			name:     "AttrLookupResult",
			attrFunc: func(v string) string { return string(AttrLookupResult(v).Key) },
			wantKey:  MetricDimensionLookupResult,
		},
		{
			name:     "AttrErrorType",
			attrFunc: func(v string) string { return string(AttrErrorType(v).Key) },
			wantKey:  MetricDimensionErrorType,
		},
		{
			name:     "AttrOutcome",
			attrFunc: func(v string) string { return string(AttrOutcome(v).Key) },
			wantKey:  MetricDimensionOutcome,
		},
		{
			name:     "AttrGuardrail",
			attrFunc: func(v string) string { return string(AttrGuardrail(v).Key) },
			wantKey:  MetricDimensionGuardrail,
		},
		{
			name:     "AttrPolicyID",
			attrFunc: func(v string) string { return string(AttrPolicyID(v).Key) },
			wantKey:  MetricDimensionPolicyID,
		},
		{
			name:     "AttrEvidenceClass",
			attrFunc: func(v string) string { return string(AttrEvidenceClass(v).Key) },
			wantKey:  MetricDimensionEvidenceClass,
		},
		{
			name:     "AttrBackendKind",
			attrFunc: func(v string) string { return string(AttrBackendKind(v).Key) },
			wantKey:  MetricDimensionBackendKind,
		},
		{
			name:     "AttrConfidence",
			attrFunc: func(v string) string { return string(AttrConfidence(v).Key) },
			wantKey:  MetricDimensionConfidence,
		},
		{
			name:     "AttrRiskType",
			attrFunc: func(v string) string { return string(AttrRiskType(v).Key) },
			wantKey:  MetricDimensionRiskType,
		},
		{
			name:     "AttrSeverity",
			attrFunc: func(v string) string { return string(AttrSeverity(v).Key) },
			wantKey:  MetricDimensionSeverity,
		},
		{
			name:     "AttrResult",
			attrFunc: func(v string) string { return string(AttrResult(v).Key) },
			wantKey:  MetricDimensionResult,
		},
		{
			name:     "AttrReason",
			attrFunc: func(v string) string { return string(AttrReason(v).Key) },
			wantKey:  MetricDimensionReason,
		},
		{
			name:     "AttrKind",
			attrFunc: func(v string) string { return string(AttrKind(v).Key) },
			wantKey:  MetricDimensionKind,
		},
		{
			name:     "AttrAction",
			attrFunc: func(v string) string { return string(AttrAction(v).Key) },
			wantKey:  MetricDimensionAction,
		},
		{
			name:     "AttrProvider",
			attrFunc: func(v string) string { return string(AttrProvider(v).Key) },
			wantKey:  MetricDimensionProvider,
		},
		{
			name:     "AttrProviderKind",
			attrFunc: func(v string) string { return string(AttrProviderKind(v).Key) },
			wantKey:  MetricDimensionProviderKind,
		},
		{
			name:     "AttrProviderProfileClass",
			attrFunc: func(v string) string { return string(AttrProviderProfileClass(v).Key) },
			wantKey:  MetricDimensionProviderProfileClass,
		},
		{
			name:     "AttrEventKind",
			attrFunc: func(v string) string { return string(AttrEventKind(v).Key) },
			wantKey:  MetricDimensionEventKind,
		},
		{
			name:     "AttrDecision",
			attrFunc: func(v string) string { return string(AttrDecision(v).Key) },
			wantKey:  MetricDimensionDecision,
		},
		{
			name:     "AttrStatus",
			attrFunc: func(v string) string { return string(AttrStatus(v).Key) },
			wantKey:  MetricDimensionStatus,
		},
		{
			name:     "AttrFailureClass",
			attrFunc: func(v string) string { return string(AttrFailureClass(v).Key) },
			wantKey:  MetricDimensionFailureClass,
		},
		{
			name:     "AttrStatusClass",
			attrFunc: func(v string) string { return string(AttrStatusClass(v).Key) },
			wantKey:  MetricDimensionStatusClass,
		},
		{
			name:     "AttrService",
			attrFunc: func(v string) string { return string(AttrService(v).Key) },
			wantKey:  MetricDimensionService,
		},
		{
			name:     "AttrAccount",
			attrFunc: func(v string) string { return string(AttrAccount(v).Key) },
			wantKey:  MetricDimensionAccount,
		},
		{
			name:     "AttrRegion",
			attrFunc: func(v string) string { return string(AttrRegion(v).Key) },
			wantKey:  MetricDimensionRegion,
		},
		{
			name:     "AttrSafeLocatorHash",
			attrFunc: func(v string) string { return string(AttrSafeLocatorHash(v).Key) },
			wantKey:  MetricDimensionSafeLocatorHash,
		},
		{
			name:     "AttrWarningKind",
			attrFunc: func(v string) string { return string(AttrWarningKind(v).Key) },
			wantKey:  MetricDimensionWarningKind,
		},
		{
			name:     "AttrResourceType",
			attrFunc: func(v string) string { return string(AttrResourceType(v).Key) },
			wantKey:  MetricDimensionResourceType,
		},
		{
			name:     "AttrBudgetState",
			attrFunc: func(v string) string { return string(AttrBudgetState(v).Key) },
			wantKey:  MetricDimensionBudgetState,
		},
		{
			name:     "AttrBudgetReason",
			attrFunc: func(v string) string { return string(AttrBudgetReason(v).Key) },
			wantKey:  MetricDimensionBudgetReason,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey := tt.attrFunc("test-value")
			assert.Equal(t, tt.wantKey, gotKey,
				"Attribute key should match contract constant")
		})
	}
}

// TestAttrPartitionID proves that AttrPartitionID returns the correct dimension
// key and preserves the integer value. It is kept separate from TestAttrHelpers
// because AttrPartitionID takes int, not string.
func TestAttrPartitionID(t *testing.T) {
	t.Parallel()

	kv := AttrPartitionID(3)
	if got := string(kv.Key); got != MetricDimensionPartitionID {
		t.Fatalf("AttrPartitionID key = %q, want %q", got, MetricDimensionPartitionID)
	}
	if got := kv.Value.AsInt64(); got != 3 {
		t.Fatalf("AttrPartitionID value = %d, want 3", got)
	}
}

func TestRegisterObservableGauges_NilInstruments(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	err := RegisterObservableGauges(nil, meter, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil instruments")
	}
}

func TestRegisterObservableGauges_NilMeter(t *testing.T) {
	inst := &Instruments{}
	err := RegisterObservableGauges(inst, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil meter")
	}
}

func TestRegisterObservableGauges_NilObservers(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	err := RegisterObservableGauges(inst, meter, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error with nil observers: %v", err)
	}
}

func TestRegisterObservableGauges_WithObservers(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}

	queueObs := &fakeQueueObserver{
		depths: map[string]map[string]int64{
			"projector": {"pending": 5, "in_flight": 2},
		},
		ages: map[string]float64{
			"projector": 30.5,
		},
	}
	workerObs := &fakeWorkerObserver{
		counts: map[string]int64{
			"collector": 3,
			"projector": 2,
		},
	}

	err := RegisterObservableGauges(inst, meter, queueObs, workerObs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.QueueDepth == nil {
		t.Error("expected QueueDepth gauge to be set")
	}
	if inst.QueueOldestAge == nil {
		t.Error("expected QueueOldestAge gauge to be set")
	}
	if inst.WorkerPoolActive == nil {
		t.Error("expected WorkerPoolActive gauge to be set")
	}
}

func TestRegisterAWSClaimConcurrencyGauge(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}

	err := RegisterAWSClaimConcurrencyGauge(inst, meter, &fakeAWSClaimConcurrencyObserver{
		counts: map[string]int64{"123456789012": 2},
	})
	if err != nil {
		t.Fatalf("RegisterAWSClaimConcurrencyGauge() error = %v", err)
	}
	if inst.AWSClaimConcurrency == nil {
		t.Fatalf("AWSClaimConcurrency gauge was not registered")
	}
}

func TestRegisterAcceptanceObservableGauges_NilInputs(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}

	if err := RegisterAcceptanceObservableGauges(nil, meter, nil); err == nil {
		t.Fatal("expected error for nil instruments")
	}
	if err := RegisterAcceptanceObservableGauges(inst, nil, nil); err == nil {
		t.Fatal("expected error for nil meter")
	}
}

func TestRegisterAcceptanceObservableGauges_WithObserver(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	observer := &fakeAcceptanceObserver{rows: 42}

	if err := RegisterAcceptanceObservableGauges(inst, meter, observer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.SharedAcceptanceRows == nil {
		t.Fatal("expected SharedAcceptanceRows gauge to be set")
	}
}

func TestRegisterGraphOrphanObservableGauge_WithObserver(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst := &Instruments{}
	observer := &fakeGraphOrphanObserver{
		counts: map[string]int64{
			"Repository": 3,
			"Platform":   1,
		},
	}

	if err := RegisterGraphOrphanObservableGauge(inst, provider.Meter("test"), observer); err != nil {
		t.Fatalf("RegisterGraphOrphanObservableGauge() error = %v", err)
	}
	if inst.GraphOrphanNodes == nil {
		t.Fatal("GraphOrphanNodes gauge was not registered")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := observableInt64GaugeValue(t, rm, "eshu_dp_graph_orphan_nodes", map[string]string{"node_label": "Repository"}); got != 3 {
		t.Fatalf("Repository orphan gauge = %d, want 3", got)
	}
	if got := observableInt64GaugeValue(t, rm, "eshu_dp_graph_orphan_nodes", map[string]string{"node_label": "Platform"}); got != 1 {
		t.Fatalf("Platform orphan gauge = %d, want 1", got)
	}
}

type fakeWorkflowFamilyQueueObserver struct {
	depths map[string]map[string]map[string]int64
}

func (f *fakeWorkflowFamilyQueueObserver) WorkflowFamilyQueueDepths(_ context.Context) (map[string]map[string]map[string]int64, error) {
	return f.depths, nil
}

func TestRegisterWorkflowFamilyQueueDepthObservableGauge_NilObserver(t *testing.T) {
	inst := &Instruments{}
	meter := sdkmetric.NewMeterProvider().Meter("test")
	if err := RegisterWorkflowFamilyQueueDepthObservableGauge(inst, meter, nil); err != nil {
		t.Fatalf("RegisterWorkflowFamilyQueueDepthObservableGauge(nil observer) error = %v", err)
	}
	if inst.WorkflowFamilyQueueDepth != nil {
		t.Fatal("gauge should not be registered for a nil observer")
	}
}

func TestRegisterWorkflowFamilyQueueDepthObservableGauge_WithObserver(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst := &Instruments{}
	observer := &fakeWorkflowFamilyQueueObserver{
		depths: map[string]map[string]map[string]int64{
			"git": {"github": {"pending": 3, "claimed": 1}},
			"aws": {"aws": {"failed_retryable": 2}},
		},
	}

	if err := RegisterWorkflowFamilyQueueDepthObservableGauge(inst, provider.Meter("test"), observer); err != nil {
		t.Fatalf("RegisterWorkflowFamilyQueueDepthObservableGauge() error = %v", err)
	}
	if inst.WorkflowFamilyQueueDepth == nil {
		t.Fatal("WorkflowFamilyQueueDepth gauge was not registered")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := observableInt64GaugeValue(t, rm, "eshu_dp_workflow_family_queue_depth", map[string]string{
		"collector_kind": "git", "source_system": "github", "status": "pending",
	}); got != 3 {
		t.Fatalf("git/github/pending gauge = %d, want 3", got)
	}
	if got := observableInt64GaugeValue(t, rm, "eshu_dp_workflow_family_queue_depth", map[string]string{
		"collector_kind": "git", "source_system": "github", "status": "claimed",
	}); got != 1 {
		t.Fatalf("git/github/claimed gauge = %d, want 1", got)
	}
	if got := observableInt64GaugeValue(t, rm, "eshu_dp_workflow_family_queue_depth", map[string]string{
		"collector_kind": "aws", "source_system": "aws", "status": "failed_retryable",
	}); got != 2 {
		t.Fatalf("aws/aws/failed_retryable gauge = %d, want 2", got)
	}
}

func TestReconciliationDriftRetractionsCounterRecordsBoundedLabels(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := NewInstruments(provider.Meter("test"))
	require.NoError(t, err)

	inst.ReconciliationDriftRetractions.Add(
		context.Background(), 3,
		metric.WithAttributes(
			AttrDomain("canonical_graph"),
			AttrWritePhase("retract"),
			AttrKind("node"),
		),
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := int64CounterValue(t, rm, "eshu_dp_reconciliation_drift_retractions_total", map[string]string{
		"domain":      "canonical_graph",
		"write_phase": "retract",
		"kind":        "node",
	}); got != 3 {
		t.Fatalf("reconciliation drift counter = %d, want 3", got)
	}
}

type fakeQueueObserver struct {
	depths map[string]map[string]int64
	ages   map[string]float64
}

func (f *fakeQueueObserver) QueueDepths(_ context.Context) (map[string]map[string]int64, error) {
	return f.depths, nil
}

func (f *fakeQueueObserver) QueueOldestAge(_ context.Context) (map[string]float64, error) {
	return f.ages, nil
}

type fakeWorkerObserver struct {
	counts map[string]int64
}

func (f *fakeWorkerObserver) ActiveWorkers(_ context.Context) (map[string]int64, error) {
	return f.counts, nil
}

type fakeAWSClaimConcurrencyObserver struct {
	counts map[string]int64
}

func (f *fakeAWSClaimConcurrencyObserver) AWSClaimConcurrency(context.Context) (map[string]int64, error) {
	return f.counts, nil
}

type fakeGraphOrphanObserver struct {
	counts map[string]int64
}

func (f *fakeGraphOrphanObserver) GraphOrphanNodeCounts(context.Context) (map[string]int64, error) {
	return f.counts, nil
}

type fakeAcceptanceObserver struct {
	rows int64
}

func (f *fakeAcceptanceObserver) AcceptanceRowCount(_ context.Context) (int64, error) {
	return f.rows, nil
}

func observableInt64GaugeValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			gauge, ok := metric.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Int64 gauge", name, metric.Data)
			}
			for _, point := range gauge.DataPoints {
				attrs := point.Attributes.ToSlice()
				matched := true
				for wantKey, wantValue := range wantAttrs {
					found := false
					for _, attr := range attrs {
						if string(attr.Key) == wantKey && attr.Value.AsString() == wantValue {
							found = true
							break
						}
					}
					if !found {
						matched = false
						break
					}
				}
				if matched {
					return point.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %#v not found", name, wantAttrs)
	return 0
}

func int64CounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Int64 sum", name, metric.Data)
			}
			for _, point := range sum.DataPoints {
				attrs := point.Attributes.ToSlice()
				matched := true
				for wantKey, wantValue := range wantAttrs {
					found := false
					for _, attr := range attrs {
						if string(attr.Key) == wantKey && attr.Value.AsString() == wantValue {
							found = true
							break
						}
					}
					if !found {
						matched = false
						break
					}
				}
				if matched {
					return point.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %#v not found", name, wantAttrs)
	return 0
}
