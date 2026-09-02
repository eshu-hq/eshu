// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorawscloudimage "github.com/eshu-hq/eshu/go/internal/projector/awscloudimage"
	projectorawsrelationship "github.com/eshu-hq/eshu/go/internal/projector/awsrelationship"
	projectorazure "github.com/eshu-hq/eshu/go/internal/projector/azure"
	projectorcloudinventory "github.com/eshu-hq/eshu/go/internal/projector/cloudinventory"
	projectorcodeinterprocevidence "github.com/eshu-hq/eshu/go/internal/projector/codeinterprocevidence"
	projectorcodetaintevidence "github.com/eshu-hq/eshu/go/internal/projector/codetaintevidence"
	projectorec2 "github.com/eshu-hq/eshu/go/internal/projector/ec2"
	projectorgcp "github.com/eshu-hq/eshu/go/internal/projector/gcp"
	projectoriamcanassume "github.com/eshu-hq/eshu/go/internal/projector/iamcanassume"
	projectoriaminstanceprofile "github.com/eshu-hq/eshu/go/internal/projector/iaminstanceprofile"
	projectorincidentrouting "github.com/eshu-hq/eshu/go/internal/projector/incidentrouting"
	projectorkubernetes "github.com/eshu-hq/eshu/go/internal/projector/kubernetes"
	projectorobservabilitycoverage "github.com/eshu-hq/eshu/go/internal/projector/observabilitycoverage"
	projectorpackagesource "github.com/eshu-hq/eshu/go/internal/projector/packagesource"
	projectorrds "github.com/eshu-hq/eshu/go/internal/projector/rds"
	projectors3 "github.com/eshu-hq/eshu/go/internal/projector/s3"
	projectorsbomattestation "github.com/eshu-hq/eshu/go/internal/projector/sbomattestation"
	projectorsecretsiam "github.com/eshu-hq/eshu/go/internal/projector/secretsiam"
	projectorsecurity "github.com/eshu-hq/eshu/go/internal/projector/security"
	projectorservicecatalog "github.com/eshu-hq/eshu/go/internal/projector/servicecatalog"
	projectorworkloadcloud "github.com/eshu-hq/eshu/go/internal/projector/workloadcloud"
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
// immutable once a scope generation is claimed for projection, so all 44
// builders can safely share the same read-only lookup. intent.NewFactLookup
// builds that lookup in two O(N) passes, first counting facts per kind and then
// filling exact-capacity position slices. Root builds it once so each builder
// can select its trigger facts without rescanning or rebuilding the index.
func appendScopeGenerationReducerIntents(
	intents []ReducerIntent,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	inputFacts []facts.Envelope,
) []ReducerIntent {
	index := newReducerIntentFactIndex(inputFacts)

	if intent, ok := projectorpackagesource.BuildPackageSourceCorrelationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAWSCloudRuntimeDriftReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildMultiCloudRuntimeDriftReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildAWSResourceMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorgcp.BuildResourceMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorgcp.BuildRelationshipMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorazure.BuildResourceMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorazure.BuildRelationshipMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorcloudinventory.BuildCloudInventoryAdmissionReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorworkloadcloud.BuildWorkloadCloudRelationshipMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorec2.BuildInstanceNodeMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorawsrelationship.BuildAWSRelationshipMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorawscloudimage.BuildAWSCloudImageMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildObservabilityCoverageMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorobservabilitycoverage.BuildObservabilityCoverageCorrelationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorincidentrouting.BuildIncidentRoutingMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorcodetaintevidence.BuildCodeTaintEvidenceReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorcodeinterprocevidence.BuildCodeInterprocEvidenceReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCodeFunctionSummaryReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectoriamcanassume.BuildIAMCanAssumeMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectors3.BuildLogsToMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectors3.BuildExternalPrincipalGrantMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorrds.BuildRDSPostureMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorec2.BuildInstanceIdentityMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorec2.BuildUsesProfileMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectoriaminstanceprofile.BuildIAMInstanceProfileRoleMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorec2.BuildInternetExposureMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorec2.BuildBlockDeviceKMSPostureMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectors3.BuildInternetExposureMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildContainerImageIdentityReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCICDRunCorrelationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorsbomattestation.BuildSBOMAttestationAttachmentReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorservicecatalog.BuildServiceCatalogCorrelationReducerIntent(scopeValue, generation, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorsecretsiam.BuildSecretsIAMTrustChainReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildSupplyChainImpactReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorsecurity.BuildSecurityAlertReconciliationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorkubernetes.BuildCorrelationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorkubernetes.BuildWorkloadMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorkubernetes.BuildNamespaceMaterializationReducerIntent(scopeValue, generation, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorkubernetes.BuildCorrelationMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := buildCrossplaneSatisfiedByMaterializationReducerIntent(scopeValue, generation, index); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorsecurity.BuildSecurityGroupEndpointMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorsecurity.BuildSecurityGroupRuleMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorsecurity.BuildSecurityGroupReachabilityMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	return intents
}
