// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// SupplyChainServiceWorkloadPair records one (ServiceID, WorkloadID) pair
// exactly as it appeared together on a single reducer_service_catalog_
// correlation fact (#5466 round-8 review F-2). It exists because
// finding.ServiceIDs/WorkloadIDs are independently flattened, deduplicated
// lists with no record of which value paired with which -- WorkloadIDs in
// particular mixes this genuinely-paired source with
// reducer_workload_identity's workload IDs, which have no known service at
// all. suppressionServiceWorkloadPairMatches
// (supply_chain_suppression_scope_match.go) uses this to verify a
// suppression scoped by BOTH workload_id and service_id names a combination
// that actually co-occurred, rather than two independently-true list
// memberships that never occurred together.
type SupplyChainServiceWorkloadPair struct {
	ServiceID  string
	WorkloadID string
}

// uniqueServiceWorkloadPairs trims, deduplicates, and sorts pairs by ServiceID
// and then WorkloadID, matching payloadcore.UniqueSortedStrings' canonical-output contract
// for the finding's other evidence-derived lists. Deliberately does
// NOT drop pairs with an empty ServiceID or WorkloadID: an empty field
// records "this service-catalog record did not resolve that identity",
// which suppressionServiceWorkloadPairMatches must still treat as
// non-matching for a scope naming that dimension (fail closed), not as a
// wildcard the way an empty SCOPE value is treated elsewhere.
func uniqueServiceWorkloadPairs(pairs []SupplyChainServiceWorkloadPair) []SupplyChainServiceWorkloadPair {
	if len(pairs) == 0 {
		return nil
	}
	seen := make(map[SupplyChainServiceWorkloadPair]struct{}, len(pairs))
	out := make([]SupplyChainServiceWorkloadPair, 0, len(pairs))
	for _, pair := range pairs {
		pair = SupplyChainServiceWorkloadPair{
			ServiceID:  strings.TrimSpace(pair.ServiceID),
			WorkloadID: strings.TrimSpace(pair.WorkloadID),
		}
		if _, ok := seen[pair]; ok {
			continue
		}
		seen[pair] = struct{}{}
		out = append(out, pair)
	}
	slices.SortFunc(out, func(a, b SupplyChainServiceWorkloadPair) int {
		if serviceOrder := strings.Compare(a.ServiceID, b.ServiceID); serviceOrder != 0 {
			return serviceOrder
		}
		return strings.Compare(a.WorkloadID, b.WorkloadID)
	})
	return out
}

func finalizeSupplyChainImpactFinding(
	finding *SupplyChainImpactFinding,
	index supplyChainImpactIndex,
	missingEvidence ...[]string,
) {
	var missing []string
	for _, values := range missingEvidence {
		missing = append(missing, values...)
	}
	missing = append(missing, applySupplyChainRuntimeContext(finding, index)...)
	finding.MissingEvidence = combinedMissingImpactEvidence(*finding, missing)
}

func applySupplyChainRuntimeContext(
	finding *SupplyChainImpactFinding,
	index supplyChainImpactIndex,
) []string {
	if finding == nil {
		return nil
	}
	var missing []string
	workloads := matchingSupplyChainWorkloads(*finding, index.workloads)
	for _, workload := range workloads {
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, workload.FactID)
		finding.EvidencePath = append(finding.EvidencePath, reducercontract.WorkloadIdentityFactKind)
		finding.WorkloadIDs = append(finding.WorkloadIDs, workload.WorkloadID)
	}
	for _, lane := range matchingSupplyChainDeploymentLanes(*finding, index.deploymentLanes) {
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, lane.FactID)
		finding.EvidencePath = append(finding.EvidencePath, reducercontract.PlatformMaterializationFactKind)
		finding.DeploymentIDs = append(finding.DeploymentIDs, lane.DeploymentIDs...)
	}
	services, serviceMissing := matchingSupplyChainServices(*finding, index.services)
	missing = append(missing, serviceMissing...)
	for _, service := range services {
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, service.FactID)
		finding.EvidencePath = append(finding.EvidencePath, reducercontract.ServiceCatalogCorrelationFactKind)
		finding.ServiceIDs = append(finding.ServiceIDs, service.ServiceID)
		finding.WorkloadIDs = append(finding.WorkloadIDs, service.WorkloadID)
		finding.ServiceWorkloadPairs = append(finding.ServiceWorkloadPairs, SupplyChainServiceWorkloadPair{
			ServiceID:  service.ServiceID,
			WorkloadID: service.WorkloadID,
		})
		finding.CatalogEntityRefs = append(finding.CatalogEntityRefs, service.EntityRef)
		finding.CatalogOwnerRefs = append(finding.CatalogOwnerRefs, service.OwnerRef)
	}
	deployments, deploymentMissing := matchingSupplyChainDeployments(*finding, index.deployments)
	missing = append(missing, deploymentMissing...)
	for _, deployment := range deployments {
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, deployment.FactID)
		finding.EvidencePath = append(finding.EvidencePath, cicdrun.CICDRunCorrelationFactKind)
		finding.Environments = append(finding.Environments, deployment.Environment)
		finding.EnvironmentEvidence = recordSupplyChainEnvironmentEvidence(
			finding.EnvironmentEvidence, deployment.Environment, deployment.EnvironmentEvidence,
		)
		if finding.RepositoryID == "" {
			finding.RepositoryID = deployment.RepositoryID
		}
	}
	bakeSupplyChainCIDeclaredArtifactIdentity(finding, deployments)
	finding.EvidenceFactIDs = payloadcore.UniqueSortedStrings(finding.EvidenceFactIDs)
	finding.EvidencePath = orderedUniqueStrings(finding.EvidencePath)
	finding.DeploymentIDs = payloadcore.UniqueSortedStrings(finding.DeploymentIDs)
	finding.Environments = payloadcore.UniqueSortedStrings(finding.Environments)
	finding.ServiceIDs = payloadcore.UniqueSortedStrings(finding.ServiceIDs)
	finding.WorkloadIDs = payloadcore.UniqueSortedStrings(finding.WorkloadIDs)
	finding.ServiceWorkloadPairs = uniqueServiceWorkloadPairs(finding.ServiceWorkloadPairs)
	finding.CatalogEntityRefs = payloadcore.UniqueSortedStrings(finding.CatalogEntityRefs)
	finding.CatalogOwnerRefs = payloadcore.UniqueSortedStrings(finding.CatalogOwnerRefs)
	if finding.RuntimeReachability != "known_fixed" &&
		finding.SubjectDigest != "" &&
		supplyChainDeploymentsPromoteRuntimeReachability(*finding, deployments) {
		finding.RuntimeReachability = "deployed_image"
	}
	return payloadcore.UniqueSortedStrings(missing)
}

func matchingSupplyChainDeployments(
	finding SupplyChainImpactFinding,
	deployments []supplychainmodel.DeploymentContext,
) ([]supplychainmodel.DeploymentContext, []string) {
	if strings.TrimSpace(finding.SubjectDigest) == "" && strings.TrimSpace(finding.ImageRef) == "" &&
		!supplyChainFindingHasOperationalAnchor(finding) {
		return nil, nil
	}
	var matches []supplychainmodel.DeploymentContext
	var rejected []string
	for _, deployment := range deployments {
		if !supplyChainDeploymentMatchesFinding(finding, deployment) {
			continue
		}
		switch deployment.Outcome {
		case string(cicdrun.CICDRunCorrelationExact), string(cicdrun.CICDRunCorrelationDerived), "":
			if deployment.ProvenanceOnly {
				rejected = append(rejected, "deployment evidence provenance-only")
				continue
			}
			matches = append(matches, deployment)
		case string(cicdrun.CICDRunCorrelationAmbiguous):
			rejected = append(rejected, "deployment evidence ambiguous")
		case string(cicdrun.CICDRunCorrelationRejected):
			rejected = append(rejected, "deployment evidence rejected")
		case string(cicdrun.CICDRunCorrelationUnresolved):
			rejected = append(rejected, "deployment evidence unresolved")
		default:
			rejected = append(rejected, "deployment evidence unsupported")
		}
	}
	return matches, payloadcore.UniqueSortedStrings(rejected)
}

func supplyChainDeploymentMatchesFinding(
	finding SupplyChainImpactFinding,
	deployment supplychainmodel.DeploymentContext,
) bool {
	if finding.SubjectDigest != "" && deployment.ArtifactDigest == finding.SubjectDigest {
		return true
	}
	if finding.ImageRef != "" && deployment.ImageRef == finding.ImageRef {
		return true
	}
	// The free-text-environment branch is the only one where the deployment
	// link is not anchored to an artifact identity (digest or image
	// reference) -- it joins purely on repository plus an operational
	// anchor. That weakness is corrected at the promotion gate
	// (supplyChainDeploymentPromotesRuntimeReachability), NOT here: the match
	// itself still carries the environment, the cicd_run_correlation evidence
	// hop, and the correlation fact ID, all of which a declared-only
	// deployment legitimately contributes.
	if finding.RepositoryID != "" && deployment.RepositoryID == finding.RepositoryID &&
		deployment.Environment != "" &&
		supplyChainFindingHasOperationalAnchor(finding) {
		return true
	}
	return false
}

// supplyChainDeploymentPromotesRuntimeReachability reports whether one matched
// deployment is strong enough to raise a finding to
// RuntimeReachability="deployed_image".
//
// A deployment qualifies when it is anchored to the finding's artifact
// identity (digest or image reference), or when its environment carries
// ci.deployment_event corroboration published by #5425. It does NOT qualify on
// the free-text-environment branch alone: that branch joins on repository plus
// an operational anchor with no artifact identity, so without corroboration a
// finding with a real digest could otherwise reach deployed_image through a
// deployment that never referenced that digest (#5426).
//
// deploy_event corroborates the ENVIRONMENT, not the ARTIFACT. A correlation
// row carries artifact_digest and environment_evidence together, so a
// deploy_event row can name a digest that contradicts the finding's own
// subject. That is positive evidence the vulnerable artifact was NOT what
// shipped -- categorically stronger than merely missing evidence -- so a
// contradicting digest disqualifies the deployment outright rather than being
// rescued by its environment corroboration.
//
// Only the digest is treated as decisive. Image references are mutable and
// registry-prefixed, so two differing refs do not reliably denote two different
// artifacts the way two differing digests do. That mutability cuts both ways:
// the contradicting-digest check runs FIRST, ahead of the image-ref branch,
// because a tag can be retagged from one digest to another. A deployment whose
// ref matches but whose digest explicitly names a different image is reporting
// that the tag has moved, and the digest is the identity worth believing.
// PRECONDITION: the caller must already have established that the finding has
// a non-empty SubjectDigest. With an empty one this falls through to the
// deploy_event check, which would promote a finding that has no artifact
// identity to compare against. applySupplyChainRuntimeContext enforces this.
func supplyChainDeploymentPromotesRuntimeReachability(
	finding SupplyChainImpactFinding,
	deployment supplychainmodel.DeploymentContext,
) bool {
	if finding.SubjectDigest != "" && deployment.ArtifactDigest != "" &&
		deployment.ArtifactDigest != finding.SubjectDigest {
		return false
	}
	if finding.SubjectDigest != "" && deployment.ArtifactDigest == finding.SubjectDigest {
		return true
	}
	if finding.ImageRef != "" && deployment.ImageRef == finding.ImageRef {
		return true
	}
	return deployment.EnvironmentEvidence == supplyChainEnvironmentEvidenceDeployEvent
}

// supplyChainDeploymentsPromoteRuntimeReachability reports whether ANY matched
// deployment justifies deployed_image. Matches that do not qualify still keep
// their environment and evidence contributions; they simply do not, on their
// own, carry the finding to deployed_image.
//
// PRECONDITION: deployments MUST already have passed
// matchingSupplyChainDeployments. This function decides promotion strength, not
// linkage, so passing unmatched rows (for example the raw index) would promote
// on any deploy_event correlation anywhere in scope. There is one caller today
// and it satisfies this; keep it that way.
func supplyChainDeploymentsPromoteRuntimeReachability(
	finding SupplyChainImpactFinding,
	deployments []supplychainmodel.DeploymentContext,
) bool {
	for _, deployment := range deployments {
		if supplyChainDeploymentPromotesRuntimeReachability(finding, deployment) {
			return true
		}
	}
	return false
}

func supplyChainFindingHasOperationalAnchor(finding SupplyChainImpactFinding) bool {
	return len(finding.WorkloadIDs) > 0 || len(finding.DeploymentIDs) > 0 || len(finding.ServiceIDs) > 0
}

func matchingSupplyChainWorkloads(
	finding SupplyChainImpactFinding,
	workloads []supplychainmodel.WorkloadContext,
) []supplychainmodel.WorkloadContext {
	repositoryID := strings.TrimSpace(finding.RepositoryID)
	if repositoryID == "" {
		return nil
	}
	matches := make([]supplychainmodel.WorkloadContext, 0, len(workloads))
	for _, workload := range workloads {
		if workload.RepositoryID != repositoryID || workload.WorkloadID == "" {
			continue
		}
		matches = append(matches, workload)
	}
	return matches
}

func matchingSupplyChainDeploymentLanes(
	finding SupplyChainImpactFinding,
	lanes []supplychainmodel.DeploymentLaneContext,
) []supplychainmodel.DeploymentLaneContext {
	repositoryID := strings.TrimSpace(finding.RepositoryID)
	if repositoryID == "" {
		return nil
	}
	matches := make([]supplychainmodel.DeploymentLaneContext, 0, len(lanes))
	for _, lane := range lanes {
		if lane.RepositoryID != repositoryID || len(lane.DeploymentIDs) == 0 {
			continue
		}
		matches = append(matches, lane)
	}
	return matches
}

func matchingSupplyChainServices(
	finding SupplyChainImpactFinding,
	services []supplychainmodel.ServiceContext,
) ([]supplychainmodel.ServiceContext, []string) {
	repositoryID := strings.TrimSpace(finding.RepositoryID)
	if repositoryID == "" {
		return nil, nil
	}
	var matches []supplychainmodel.ServiceContext
	var rejected []string
	for _, service := range services {
		if service.RepositoryID != repositoryID {
			continue
		}
		switch service.Outcome {
		case string(servicecatalog.ServiceCatalogCorrelationExact), string(servicecatalog.ServiceCatalogCorrelationDerived), "":
			if service.ProvenanceOnly {
				rejected = append(rejected, "service catalog evidence provenance-only")
				continue
			}
			if service.ServiceID == "" && service.WorkloadID == "" &&
				!supplyChainServiceCatalogContextHasResolvedAnchor(finding, service) {
				rejected = append(rejected, "service/workload catalog anchor missing")
			}
			matches = append(matches, service)
		case string(servicecatalog.ServiceCatalogCorrelationStale):
			rejected = append(rejected, "service catalog evidence stale")
		case string(servicecatalog.ServiceCatalogCorrelationAmbiguous):
			rejected = append(rejected, "service catalog evidence ambiguous")
		case string(servicecatalog.ServiceCatalogCorrelationRejected):
			rejected = append(rejected, "service catalog evidence rejected")
		case string(servicecatalog.ServiceCatalogCorrelationUnresolved):
			rejected = append(rejected, "service catalog evidence unresolved")
		default:
			rejected = append(rejected, "service catalog evidence unsupported")
		}
	}
	return matches, payloadcore.UniqueSortedStrings(rejected)
}

func supplyChainServiceCatalogContextHasResolvedAnchor(
	finding SupplyChainImpactFinding,
	service supplychainmodel.ServiceContext,
) bool {
	return len(finding.WorkloadIDs) > 0 && service.EntityRef != ""
}
