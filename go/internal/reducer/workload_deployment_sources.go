package reducer

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

type deploymentSourceMetadata struct {
	repoID     string
	confidence float64
	provenance string
}

func applyResolvedDeploymentSources(
	candidates []WorkloadCandidate,
	resolved []relationships.ResolvedRelationship,
) []WorkloadCandidate {
	if len(candidates) == 0 || len(resolved) == 0 {
		return candidates
	}

	deploymentReposBySource := make(map[string][]deploymentSourceMetadata, len(resolved))
	for _, relationship := range resolved {
		if relationship.RelationshipType != relationships.RelDeploysFrom {
			continue
		}
		if relationship.SourceRepoID == "" || relationship.TargetRepoID == "" {
			continue
		}
		if !hasDeploymentEvidence(relationship.Details) {
			continue
		}
		provenance := argoDeploymentProvenance(relationship.Details)
		if provenance == "" {
			provenance = "argocd_application_source"
		}

		// Direction depends on evidence kind:
		// - ArgoCD: source=app, target=deploy_repo (app defines where it deploys from)
		// - Kustomize/Helm: source=deploy_repo, target=app (deploy repo references app)
		appRepoID, deployRepoID := relationship.SourceRepoID, relationship.TargetRepoID
		if isDeployRepoOriginatedEvidence(relationship.Details) {
			appRepoID, deployRepoID = relationship.TargetRepoID, relationship.SourceRepoID
		}

		metadata := deploymentSourceMetadata{
			repoID:     deployRepoID,
			confidence: relationship.Confidence,
			provenance: provenance,
		}
		deploymentReposBySource[appRepoID] = appendDeploymentSourceMetadata(
			deploymentReposBySource[appRepoID],
			metadata,
		)
	}

	if len(deploymentReposBySource) == 0 {
		return candidates
	}

	enriched := make([]WorkloadCandidate, len(candidates))
	for i, candidate := range candidates {
		enriched[i] = candidate
		for _, metadata := range deploymentReposBySource[candidate.RepoID] {
			enriched[i].DeploymentRepoIDs = appendUniqueString(enriched[i].DeploymentRepoIDs, metadata.repoID)
			if enriched[i].DeploymentRepoID == "" || metadata.confidence > enriched[i].Confidence {
				enriched[i].DeploymentRepoID = metadata.repoID
			}
			if metadata.confidence > enriched[i].Confidence {
				enriched[i].Confidence = metadata.confidence
			}
			enriched[i].Provenance = appendUniqueString(enriched[i].Provenance, metadata.provenance)
		}
		if len(deploymentReposBySource[candidate.RepoID]) > 0 {
			enriched[i].Classification = InferWorkloadClassification(enriched[i])
		}
	}

	return enriched
}

func appendDeploymentSourceMetadata(existing []deploymentSourceMetadata, candidate deploymentSourceMetadata) []deploymentSourceMetadata {
	if candidate.repoID == "" {
		return existing
	}
	for i, item := range existing {
		if item.repoID != candidate.repoID {
			continue
		}
		if candidate.confidence > item.confidence {
			existing[i] = candidate
		}
		return existing
	}
	return append(existing, candidate)
}

func applyResolvedProvisioningSources(
	candidates []WorkloadCandidate,
	resolved []relationships.ResolvedRelationship,
) []WorkloadCandidate {
	if len(candidates) == 0 || len(resolved) == 0 {
		return candidates
	}

	provisioningReposByTarget := make(map[string][]string, len(resolved))
	provisioningEvidenceKindsByTarget := make(map[string]map[string][]string, len(resolved))
	for _, relationship := range resolved {
		if relationship.RelationshipType != relationships.RelProvisionsDependencyFor {
			continue
		}
		if relationship.SourceRepoID == "" || relationship.TargetRepoID == "" {
			continue
		}
		provisioningReposByTarget[relationship.TargetRepoID] = appendUniqueString(
			provisioningReposByTarget[relationship.TargetRepoID],
			relationship.SourceRepoID,
		)
		for _, evidenceKind := range toStringSlice(relationship.Details["evidence_kinds"]) {
			if provisioningEvidenceKindsByTarget[relationship.TargetRepoID] == nil {
				provisioningEvidenceKindsByTarget[relationship.TargetRepoID] = make(map[string][]string)
			}
			provisioningEvidenceKindsByTarget[relationship.TargetRepoID][relationship.SourceRepoID] = appendUniqueString(
				provisioningEvidenceKindsByTarget[relationship.TargetRepoID][relationship.SourceRepoID],
				evidenceKind,
			)
		}
	}
	if len(provisioningReposByTarget) == 0 {
		return candidates
	}

	enriched := make([]WorkloadCandidate, len(candidates))
	for i, candidate := range candidates {
		enriched[i] = candidate
		enriched[i].ProvisioningEvidenceKinds = cloneStringSliceMap(candidate.ProvisioningEvidenceKinds)
		for _, repoID := range provisioningReposByTarget[candidate.RepoID] {
			enriched[i].ProvisioningRepoIDs = appendUniqueString(enriched[i].ProvisioningRepoIDs, repoID)
			for _, evidenceKind := range provisioningEvidenceKindsByTarget[candidate.RepoID][repoID] {
				if enriched[i].ProvisioningEvidenceKinds == nil {
					enriched[i].ProvisioningEvidenceKinds = make(map[string][]string)
				}
				enriched[i].ProvisioningEvidenceKinds[repoID] = appendUniqueString(
					enriched[i].ProvisioningEvidenceKinds[repoID],
					evidenceKind,
				)
			}
		}
	}

	return enriched
}

// hasDeploymentEvidence returns true when the resolved relationship carries
// evidence kinds that indicate a deployment source linkage (ArgoCD, Kustomize,
// or Helm).
func hasDeploymentEvidence(details map[string]any) bool {
	rawKinds, ok := details["evidence_kinds"]
	if !ok {
		return false
	}

	for _, kind := range toStringSlice(rawKinds) {
		switch relationships.EvidenceKind(kind) {
		case relationships.EvidenceKindArgoCDAppSource,
			relationships.EvidenceKindArgoCDApplicationSetDeploySource,
			relationships.EvidenceKindKustomizeResource,
			relationships.EvidenceKindHelmValues,
			relationships.EvidenceKindHelmChart:
			return true
		}
	}

	return false
}

// isDeployRepoOriginatedEvidence returns true when the evidence was discovered
// inside the deployment repo (Kustomize/Helm), meaning the source is the deploy
// repo and target is the app. ArgoCD evidence originates in the app repo.
func isDeployRepoOriginatedEvidence(details map[string]any) bool {
	rawKinds, ok := details["evidence_kinds"]
	if !ok {
		return false
	}
	for _, kind := range toStringSlice(rawKinds) {
		switch relationships.EvidenceKind(kind) {
		case relationships.EvidenceKindKustomizeResource,
			relationships.EvidenceKindHelmValues,
			relationships.EvidenceKindHelmChart:
			return true
		}
	}
	return false
}

func argoDeploymentProvenance(details map[string]any) string {
	for _, kind := range toStringSlice(details["evidence_kinds"]) {
		switch relationships.EvidenceKind(kind) {
		case relationships.EvidenceKindArgoCDApplicationSetDeploySource:
			return "argocd_applicationset_deploy_source"
		case relationships.EvidenceKindArgoCDAppSource:
			return "argocd_application_source"
		case relationships.EvidenceKindKustomizeResource:
			return "kustomize_resource"
		case relationships.EvidenceKindHelmValues, relationships.EvidenceKindHelmChart:
			return "helm_deployment"
		}
	}
	return ""
}

func toStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text := fmt.Sprint(item)
			if text == "" || text == "<nil>" {
				continue
			}
			result = append(result, text)
		}
		return result
	default:
		text := fmt.Sprint(value)
		if text == "" || text == "<nil>" {
			return nil
		}
		return []string{text}
	}
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func cloneStringSliceMap(values map[string][]string) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(values))
	for key, slice := range values {
		cloned[key] = append([]string(nil), slice...)
	}
	return cloned
}
