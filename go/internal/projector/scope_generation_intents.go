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
func appendScopeGenerationReducerIntents(
	intents []ReducerIntent,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	inputFacts []facts.Envelope,
) []ReducerIntent {
	if intent, ok := buildPackageSourceCorrelationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAWSCloudRuntimeDriftReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAWSResourceMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildGCPResourceMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildGCPRelationshipMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAzureResourceMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAzureRelationshipMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCloudInventoryAdmissionReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildWorkloadCloudRelationshipMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildEC2InstanceNodeMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAWSRelationshipMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildObservabilityCoverageMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildObservabilityCoverageCorrelationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildIncidentRoutingMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCodeTaintEvidenceReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCodeInterprocEvidenceReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCodeFunctionSummaryReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildIAMCanAssumeMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildS3LogsToMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildS3ExternalPrincipalGrantMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildRDSPostureMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildEC2UsesProfileMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildIAMInstanceProfileRoleMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildEC2InternetExposureMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildEC2BlockDeviceKMSPostureMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildS3InternetExposureMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildContainerImageIdentityReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSBOMAttestationAttachmentReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildServiceCatalogCorrelationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecretsIAMTrustChainReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSupplyChainImpactReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecurityAlertReconciliationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildKubernetesCorrelationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecurityGroupEndpointMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecurityGroupRuleMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSecurityGroupReachabilityMaterializationReducerIntent(scopeValue, generation, inputFacts); ok {
		intents = append(intents, intent)
	}
	return intents
}
