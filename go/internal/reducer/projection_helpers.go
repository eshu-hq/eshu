// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/environment"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func addAPIEndpointRows(
	result *ProjectionResult,
	candidate WorkloadCandidate,
	workloadID string,
	workloadName string,
	seen map[string]int,
) {
	for _, endpoint := range candidate.APIEndpoints {
		path := strings.TrimSpace(endpoint.Path)
		if path == "" {
			continue
		}
		endpointID := stableAPIEndpointID(candidate.RepoID, workloadID, path)
		if index, ok := seen[endpointID]; ok {
			existing := result.EndpointRows[index]
			existing.Methods = uniqueSortedStrings(append(existing.Methods, endpoint.Methods...))
			existing.OperationIDs = uniqueSortedStrings(append(existing.OperationIDs, endpoint.OperationIDs...))
			existing.SourceKinds = uniqueSortedStrings(append(existing.SourceKinds, endpoint.SourceKinds...))
			existing.SourcePaths = uniqueSortedStrings(append(existing.SourcePaths, endpoint.SourcePaths...))
			existing.SpecVersions = uniqueSortedStrings(append(existing.SpecVersions, endpoint.SpecVersions...))
			existing.APIVersions = uniqueSortedStrings(append(existing.APIVersions, endpoint.APIVersions...))
			result.EndpointRows[index] = existing
			continue
		}
		seen[endpointID] = len(result.EndpointRows)
		result.EndpointRows = append(result.EndpointRows, APIEndpointRow{
			EndpointID:   endpointID,
			RepoID:       candidate.RepoID,
			WorkloadID:   workloadID,
			WorkloadName: workloadName,
			Path:         path,
			Methods:      uniqueSortedStrings(endpoint.Methods),
			OperationIDs: uniqueSortedStrings(endpoint.OperationIDs),
			SourceKinds:  uniqueSortedStrings(endpoint.SourceKinds),
			SourcePaths:  uniqueSortedStrings(endpoint.SourcePaths),
			SpecVersions: uniqueSortedStrings(endpoint.SpecVersions),
			APIVersions:  uniqueSortedStrings(endpoint.APIVersions),
		})
		result.Stats.Endpoints++
	}
}

func stableAPIEndpointID(repoID, workloadID, path string) string {
	digest := sha256.Sum256([]byte(repoID + "|" + workloadID + "|" + path))
	return "endpoint:" + hex.EncodeToString(digest[:12])
}

func candidateDeploymentRepoIDs(candidate WorkloadCandidate) []string {
	repoIDs := make([]string, 0, 1+len(candidate.DeploymentRepoIDs))
	repoIDs = appendUniqueString(repoIDs, strings.TrimSpace(candidate.DeploymentRepoID))
	for _, repoID := range candidate.DeploymentRepoIDs {
		repoIDs = appendUniqueString(repoIDs, strings.TrimSpace(repoID))
	}
	return repoIDs
}

func deploymentRepoHasEnvironment(deploymentEnvironments map[string][]string, repoID string, environment string) bool {
	environments := deploymentEnvironments[repoID]
	if len(environments) == 0 {
		return true
	}
	for _, candidate := range environments {
		if candidate == environment {
			return true
		}
	}
	return false
}

func provisionedRuntimePlatformRows(
	candidate WorkloadCandidate,
	workloadName string,
	confidence float64,
	deploymentEnvironments map[string][]string,
	infrastructurePlatforms map[string][]InfrastructurePlatformRow,
) []RuntimePlatformRow {
	if len(candidate.ProvisioningRepoIDs) == 0 || len(infrastructurePlatforms) == 0 {
		return nil
	}
	var rows []RuntimePlatformRow
	for _, repoID := range candidate.ProvisioningRepoIDs {
		platforms := infrastructurePlatforms[repoID]
		if !hasRuntimeProvisioningEvidence(candidate.ProvisioningEvidenceKinds[repoID]) {
			continue
		}
		environments := deploymentEnvironments[repoID]
		if len(environments) == 0 {
			continue
		}
		for _, platform := range platforms {
			if platform.PlatformID == "" || platform.PlatformKind == "" {
				continue
			}
			for _, environment := range environments {
				instanceID := fmt.Sprintf("workload-instance:%s:%s", workloadName, environment)
				rows = append(rows, RuntimePlatformRow{
					Environment:      environment,
					Confidence:       confidence,
					InstanceID:       instanceID,
					PlatformID:       platform.PlatformID,
					PlatformKind:     platform.PlatformKind,
					PlatformName:     platform.PlatformName,
					PlatformProvider: platform.PlatformProvider,
					PlatformRegion:   platform.PlatformRegion,
					PlatformLocator:  platform.PlatformLocator,
					RepoID:           candidate.RepoID,
				})
			}
		}
	}
	return rows
}

func hasRuntimeProvisioningEvidence(kinds []string) bool {
	for _, kind := range kinds {
		normalized := strings.ToUpper(strings.TrimSpace(kind))
		if normalized == "" {
			continue
		}
		if !strings.HasPrefix(normalized, "TERRAFORM_") {
			continue
		}
		switch relationships.EvidenceKind(normalized) {
		case relationships.EvidenceKindTerraformAppRepo,
			relationships.EvidenceKindTerraformAppName,
			relationships.EvidenceKindTerraformGitHubRepo,
			relationships.EvidenceKindTerraformGitHubActions,
			relationships.EvidenceKindTerraformConfigPath,
			relationships.EvidenceKindTerraformModuleSource:
			continue
		default:
			return true
		}
	}
	return false
}

func isMaterializableWorkloadClassification(classification string) bool {
	switch strings.TrimSpace(strings.ToLower(classification)) {
	case "service", "job":
		return true
	default:
		return false
	}
}

func inferCandidateRuntimePlatformKind(candidate WorkloadCandidate) string {
	if kind := InferRuntimePlatformKind(candidate.ResourceKinds); kind != "" {
		return kind
	}
	if len(candidateDeploymentRepoIDs(candidate)) == 0 {
		return ""
	}
	if hasProvenance(
		candidate.Provenance,
		"argocd_application_source",
		"argocd_applicationset_deploy_source",
		"kustomize_resource",
		"helm_deployment",
	) {
		return "kubernetes"
	}
	return ""
}

func normalizedCandidateConfidence(confidence float64) float64 {
	if confidence > 0 {
		return confidence
	}
	return 0
}

func namespaceEnvironmentFallback(namespace string) string {
	namespace = environment.Normalize(namespace)
	if namespace == "" {
		return ""
	}
	switch namespace {
	case "prod", "production", "qa", "stage", "staging", "dev", "development", "test", "sandbox", "preview":
		return environment.Canonical(namespace)
	default:
		return ""
	}
}

func candidateWorkloadName(candidate WorkloadCandidate) string {
	if name := strings.TrimSpace(candidate.WorkloadName); name != "" {
		return name
	}
	return strings.TrimSpace(candidate.RepoName)
}

func hasAnyResourceKind(resourceKinds []string, wanted ...string) bool {
	for _, kind := range resourceKinds {
		normalized := strings.ToLower(strings.TrimSpace(kind))
		for _, candidate := range wanted {
			if normalized == candidate {
				return true
			}
		}
	}
	return false
}

func hasProvenance(provenance []string, wanted ...string) bool {
	for _, value := range provenance {
		normalized := strings.ToLower(strings.TrimSpace(value))
		for _, candidate := range wanted {
			if normalized == candidate {
				return true
			}
		}
	}
	return false
}
