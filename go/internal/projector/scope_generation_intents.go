// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/access/posture"
	awsimage "github.com/eshu-hq/eshu/go/internal/projector/aws/cloud/image"
	"github.com/eshu-hq/eshu/go/internal/projector/aws/ec2"
	"github.com/eshu-hq/eshu/go/internal/projector/aws/rds"
	"github.com/eshu-hq/eshu/go/internal/projector/aws/relationship"
	"github.com/eshu-hq/eshu/go/internal/projector/aws/resource"
	"github.com/eshu-hq/eshu/go/internal/projector/aws/s3"
	projectorazure "github.com/eshu-hq/eshu/go/internal/projector/azure"
	"github.com/eshu-hq/eshu/go/internal/projector/cicd/run/correlation"
	iamprofile "github.com/eshu-hq/eshu/go/internal/projector/cloud/aws/iam/instance/profile"
	iamtrust "github.com/eshu-hq/eshu/go/internal/projector/cloud/aws/iam/trust"
	inventory "github.com/eshu-hq/eshu/go/internal/projector/cloud/inventory"
	awsdrift "github.com/eshu-hq/eshu/go/internal/projector/cloud/runtime/drift/aws"
	multidrift "github.com/eshu-hq/eshu/go/internal/projector/cloud/runtime/drift/multi"
	summary "github.com/eshu-hq/eshu/go/internal/projector/code/function/summary"
	interproc "github.com/eshu-hq/eshu/go/internal/projector/code/interproc/evidence"
	taint "github.com/eshu-hq/eshu/go/internal/projector/code/taint/evidence"
	containerimageidentity "github.com/eshu-hq/eshu/go/internal/projector/container/image/identity"
	projectorcrossplanesatisfaction "github.com/eshu-hq/eshu/go/internal/projector/crossplane/satisfaction"
	projectorgcp "github.com/eshu-hq/eshu/go/internal/projector/gcp"
	"github.com/eshu-hq/eshu/go/internal/projector/incident/routing"
	projectorkubernetes "github.com/eshu-hq/eshu/go/internal/projector/kubernetes"
	"github.com/eshu-hq/eshu/go/internal/projector/observability/coverage"
	"github.com/eshu-hq/eshu/go/internal/projector/observability/coverage/materialization"
	packagesource "github.com/eshu-hq/eshu/go/internal/projector/package/source"
	projectorsbomattestation "github.com/eshu-hq/eshu/go/internal/projector/sbomattestation"
	projectorsecurity "github.com/eshu-hq/eshu/go/internal/projector/security"
	"github.com/eshu-hq/eshu/go/internal/projector/service/catalog"
	"github.com/eshu-hq/eshu/go/internal/projector/supply/chain/impact"
	workload "github.com/eshu-hq/eshu/go/internal/projector/workload/cloud"
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

	if intent, ok := packagesource.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := awsdrift.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := multidrift.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := resource.BuildMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
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
	if intent, ok := inventory.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := workload.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := ec2.BuildInstanceNodeMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := relationship.BuildMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := awsimage.BuildMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := materialization.BuildObservabilityCoverageMaterializationReducerIntent(scopeValue, generation, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := coverage.BuildObservabilityCoverageCorrelationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := routing.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := taint.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := interproc.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := summary.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := iamtrust.BuildIAMCanAssumeMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := s3.BuildLogsToMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := s3.BuildExternalPrincipalGrantMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := rds.BuildPostureMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := ec2.BuildInstanceIdentityMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := ec2.BuildUsesProfileMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := iamprofile.BuildIAMInstanceProfileRoleMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := ec2.BuildInternetExposureMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := ec2.BuildBlockDeviceKMSPostureMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := s3.BuildInternetExposureMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := containerimageidentity.BuildContainerImageIdentityReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := correlation.BuildReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := projectorsbomattestation.BuildSBOMAttestationAttachmentReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := catalog.BuildServiceCatalogCorrelationReducerIntent(scopeValue, generation, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := posture.BuildSecretsIAMTrustChainReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
		intents = append(intents, intent)
	}
	if intent, ok := impact.BuildSupplyChainImpactReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
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
	if intent, ok := projectorcrossplanesatisfaction.BuildCrossplaneSatisfiedByMaterializationReducerIntent(scopeValue.ScopeID, generation.GenerationID, index.lookup); ok {
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
