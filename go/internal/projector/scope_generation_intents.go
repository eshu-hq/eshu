// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// appendScopeGenerationReducerIntents appends the scope-generation-level reducer
// intents — correlation, materialization, and projection follow-ups that key off
// the full input-fact set for one scope generation rather than a single fact.
// Each builder returns at most one scope-keyed intent and the order here is not
// significant: the caller sorts the assembled intents deterministically before
// enqueue. Keeping this sequence in its own file keeps runtime.go's projection
// assembly under the file-size cap as new provider materialization paths land.
//
// It builds one shared reducerIntentFactIndex over inputFacts and passes it to
// every builder below instead of the raw slice (issue #4875): inputFacts is
// immutable once a scope generation is claimed for projection, so the 38
// builders that used to each independently re-scan the full slice can safely
// share one read-only, pre-grouped index built in a single O(N) pass.
func appendScopeGenerationReducerIntents(
	intents []ReducerIntent,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	inputFacts []facts.Envelope,
) []ReducerIntent {
	index := newReducerIntentFactIndex(inputFacts)

	if intent, ok := buildPackageSourceCorrelationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAWSCloudRuntimeDriftReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAWSResourceMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildGCPResourceMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildGCPRelationshipMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAzureResourceMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAzureRelationshipMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCloudInventoryAdmissionReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildWorkloadCloudRelationshipMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildEC2InstanceNodeMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAWSRelationshipMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAWSCloudImageMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildObservabilityCoverageMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildObservabilityCoverageCorrelationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildIncidentRoutingMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCodeTaintEvidenceReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCodeInterprocEvidenceReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCodeFunctionSummaryReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildIAMCanAssumeMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildS3LogsToMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildS3ExternalPrincipalGrantMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildRDSPostureMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildEC2InstanceIdentityMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildEC2UsesProfileMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildIAMInstanceProfileRoleMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildEC2InternetExposureMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildEC2BlockDeviceKMSPostureMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildS3InternetExposureMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildContainerImageIdentityReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSBOMAttestationAttachmentReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildServiceCatalogCorrelationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecretsIAMTrustChainReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSupplyChainImpactReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecurityAlertReconciliationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildKubernetesCorrelationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildKubernetesWorkloadMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildKubernetesNamespaceMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildKubernetesCorrelationMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCrossplaneSatisfiedByMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecurityGroupEndpointMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecurityGroupRuleMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecurityGroupReachabilityMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	return intents
}
