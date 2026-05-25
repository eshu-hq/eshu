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
	services, serviceMissing := matchingSupplyChainServices(*finding, index.services)
	missing = append(missing, serviceMissing...)
	for _, service := range services {
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, service.factID)
		finding.EvidencePath = append(finding.EvidencePath, serviceCatalogCorrelationFactKind)
		finding.ServiceIDs = append(finding.ServiceIDs, service.serviceID)
		finding.WorkloadIDs = append(finding.WorkloadIDs, service.workloadID)
	}
	finding.EvidenceFactIDs = uniqueSortedStrings(finding.EvidenceFactIDs)
	finding.EvidencePath = orderedUniqueStrings(finding.EvidencePath)
	finding.Environments = uniqueSortedStrings(finding.Environments)
	finding.ServiceIDs = uniqueSortedStrings(finding.ServiceIDs)
	finding.WorkloadIDs = uniqueSortedStrings(finding.WorkloadIDs)
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
	if strings.TrimSpace(finding.SubjectDigest) == "" && strings.TrimSpace(finding.ImageRef) == "" {
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
	return false
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
