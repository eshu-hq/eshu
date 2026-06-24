// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

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
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, workload.factID)
		finding.EvidencePath = append(finding.EvidencePath, workloadIdentityFactKind)
		finding.WorkloadIDs = append(finding.WorkloadIDs, workload.workloadID)
	}
	for _, lane := range matchingSupplyChainDeploymentLanes(*finding, index.deploymentLanes) {
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, lane.factID)
		finding.EvidencePath = append(finding.EvidencePath, platformMaterializationFactKind)
		finding.DeploymentIDs = append(finding.DeploymentIDs, lane.deploymentIDs...)
	}
	services, serviceMissing := matchingSupplyChainServices(*finding, index.services)
	missing = append(missing, serviceMissing...)
	for _, service := range services {
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, service.factID)
		finding.EvidencePath = append(finding.EvidencePath, serviceCatalogCorrelationFactKind)
		finding.ServiceIDs = append(finding.ServiceIDs, service.serviceID)
		finding.WorkloadIDs = append(finding.WorkloadIDs, service.workloadID)
		finding.CatalogEntityRefs = append(finding.CatalogEntityRefs, service.entityRef)
		finding.CatalogOwnerRefs = append(finding.CatalogOwnerRefs, service.ownerRef)
	}
	deployments, deploymentMissing := matchingSupplyChainDeployments(*finding, index.deployments)
	missing = append(missing, deploymentMissing...)
	for _, deployment := range deployments {
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, deployment.factID)
		finding.EvidencePath = append(finding.EvidencePath, cicdRunCorrelationFactKind)
		finding.Environments = append(finding.Environments, deployment.environment)
		if finding.RepositoryID == "" {
			finding.RepositoryID = deployment.repositoryID
		}
	}
	finding.EvidenceFactIDs = uniqueSortedStrings(finding.EvidenceFactIDs)
	finding.EvidencePath = orderedUniqueStrings(finding.EvidencePath)
	finding.DeploymentIDs = uniqueSortedStrings(finding.DeploymentIDs)
	finding.Environments = uniqueSortedStrings(finding.Environments)
	finding.ServiceIDs = uniqueSortedStrings(finding.ServiceIDs)
	finding.WorkloadIDs = uniqueSortedStrings(finding.WorkloadIDs)
	finding.CatalogEntityRefs = uniqueSortedStrings(finding.CatalogEntityRefs)
	finding.CatalogOwnerRefs = uniqueSortedStrings(finding.CatalogOwnerRefs)
	if finding.RuntimeReachability != "known_fixed" &&
		finding.SubjectDigest != "" && len(deployments) > 0 {
		finding.RuntimeReachability = "deployed_image"
	}
	return uniqueSortedStrings(missing)
}

func matchingSupplyChainDeployments(
	finding SupplyChainImpactFinding,
	deployments []supplyChainDeploymentContext,
) ([]supplyChainDeploymentContext, []string) {
	if strings.TrimSpace(finding.SubjectDigest) == "" && strings.TrimSpace(finding.ImageRef) == "" &&
		!supplyChainFindingHasOperationalAnchor(finding) {
		return nil, nil
	}
	var matches []supplyChainDeploymentContext
	var rejected []string
	for _, deployment := range deployments {
		if !supplyChainDeploymentMatchesFinding(finding, deployment) {
			continue
		}
		switch deployment.outcome {
		case string(CICDRunCorrelationExact), string(CICDRunCorrelationDerived), "":
			if deployment.provenanceOnly {
				rejected = append(rejected, "deployment evidence provenance-only")
				continue
			}
			matches = append(matches, deployment)
		case string(CICDRunCorrelationAmbiguous):
			rejected = append(rejected, "deployment evidence ambiguous")
		case string(CICDRunCorrelationRejected):
			rejected = append(rejected, "deployment evidence rejected")
		case string(CICDRunCorrelationUnresolved):
			rejected = append(rejected, "deployment evidence unresolved")
		default:
			rejected = append(rejected, "deployment evidence unsupported")
		}
	}
	return matches, uniqueSortedStrings(rejected)
}

func supplyChainDeploymentMatchesFinding(
	finding SupplyChainImpactFinding,
	deployment supplyChainDeploymentContext,
) bool {
	if finding.SubjectDigest != "" && deployment.artifactDigest == finding.SubjectDigest {
		return true
	}
	if finding.ImageRef != "" && deployment.imageRef == finding.ImageRef {
		return true
	}
	if finding.RepositoryID != "" && deployment.repositoryID == finding.RepositoryID &&
		deployment.environment != "" && supplyChainFindingHasOperationalAnchor(finding) {
		return true
	}
	return false
}

func supplyChainFindingHasOperationalAnchor(finding SupplyChainImpactFinding) bool {
	return len(finding.WorkloadIDs) > 0 || len(finding.DeploymentIDs) > 0 || len(finding.ServiceIDs) > 0
}

func matchingSupplyChainWorkloads(
	finding SupplyChainImpactFinding,
	workloads []supplyChainWorkloadContext,
) []supplyChainWorkloadContext {
	repositoryID := strings.TrimSpace(finding.RepositoryID)
	if repositoryID == "" {
		return nil
	}
	matches := make([]supplyChainWorkloadContext, 0, len(workloads))
	for _, workload := range workloads {
		if workload.repositoryID != repositoryID || workload.workloadID == "" {
			continue
		}
		matches = append(matches, workload)
	}
	return matches
}

func matchingSupplyChainDeploymentLanes(
	finding SupplyChainImpactFinding,
	lanes []supplyChainDeploymentLaneContext,
) []supplyChainDeploymentLaneContext {
	repositoryID := strings.TrimSpace(finding.RepositoryID)
	if repositoryID == "" {
		return nil
	}
	matches := make([]supplyChainDeploymentLaneContext, 0, len(lanes))
	for _, lane := range lanes {
		if lane.repositoryID != repositoryID || len(lane.deploymentIDs) == 0 {
			continue
		}
		matches = append(matches, lane)
	}
	return matches
}

func matchingSupplyChainServices(
	finding SupplyChainImpactFinding,
	services []supplyChainServiceContext,
) ([]supplyChainServiceContext, []string) {
	repositoryID := strings.TrimSpace(finding.RepositoryID)
	if repositoryID == "" {
		return nil, nil
	}
	var matches []supplyChainServiceContext
	var rejected []string
	for _, service := range services {
		if service.repositoryID != repositoryID {
			continue
		}
		switch service.outcome {
		case string(ServiceCatalogCorrelationExact), string(ServiceCatalogCorrelationDerived), "":
			if service.provenanceOnly {
				rejected = append(rejected, "service catalog evidence provenance-only")
				continue
			}
			if service.serviceID == "" && service.workloadID == "" &&
				!supplyChainServiceCatalogContextHasResolvedAnchor(finding, service) {
				rejected = append(rejected, "service/workload catalog anchor missing")
			}
			matches = append(matches, service)
		case string(ServiceCatalogCorrelationStale):
			rejected = append(rejected, "service catalog evidence stale")
		case string(ServiceCatalogCorrelationAmbiguous):
			rejected = append(rejected, "service catalog evidence ambiguous")
		case string(ServiceCatalogCorrelationRejected):
			rejected = append(rejected, "service catalog evidence rejected")
		case string(ServiceCatalogCorrelationUnresolved):
			rejected = append(rejected, "service catalog evidence unresolved")
		default:
			rejected = append(rejected, "service catalog evidence unsupported")
		}
	}
	return matches, uniqueSortedStrings(rejected)
}

func supplyChainServiceCatalogContextHasResolvedAnchor(
	finding SupplyChainImpactFinding,
	service supplyChainServiceContext,
) bool {
	return len(finding.WorkloadIDs) > 0 && service.entityRef != ""
}
