package reducer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// EvidenceSourceWorkloads identifies workload finalization evidence emitted by
// reducer-owned projection.
const EvidenceSourceWorkloads = "finalization/workloads"

var environmentPathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:^|/)overlays/([^/]+)/`),
	regexp.MustCompile(`(?:^|/)(?:env|environments)/([^/]+)/`),
	regexp.MustCompile(`(?:^|/)inventory/([^/.]+)(?:\.[^/]+)?$`),
	regexp.MustCompile(`(?:^|/)group_vars/([^/.]+)(?:\.[^/]+)?$`),
}

const workloadMaterializationMinConfidence = 0.82

// WorkloadCandidate holds one candidate row for workload projection. This is
// the Go equivalent of the Python candidate_rows dict produced by
// _load_candidate_rows.
type WorkloadCandidate struct {
	RepoID           string
	RepoName         string
	WorkloadName     string
	ResourceKinds    []string
	Namespaces       []string
	DeploymentRepoID string
	// DeploymentRepoIDs preserves all deployment/config repositories that
	// source this workload. DeploymentRepoID remains the compatibility primary.
	DeploymentRepoIDs   []string
	ProvisioningRepoIDs []string
	// ProvisioningEvidenceKinds keeps per-provisioning-repo evidence so
	// dependency infrastructure does not become runtime truth by default.
	ProvisioningEvidenceKinds map[string][]string
	Classification            string
	Confidence                float64
	Provenance                []string
	APIEndpoints              []APIEndpointSignal
}

// APIEndpointSignal is source evidence for one externally visible API route
// detected from parsed framework semantics or API specification files.
type APIEndpointSignal struct {
	Path         string
	Methods      []string
	OperationIDs []string
	SourceKinds  []string
	SourcePaths  []string
	SpecVersions []string
	APIVersions  []string
}

// ProjectionStats tracks counts produced during projection row building.
type ProjectionStats struct {
	Workloads         int
	Instances         int
	DeploymentSources int
	Endpoints         int
}

// WorkloadRow is one canonical workload upsert payload.
type WorkloadRow struct {
	RepoID         string
	WorkloadID     string
	WorkloadKind   string
	WorkloadName   string
	Classification string
	Confidence     float64
	Provenance     []string
}

// InstanceRow is one canonical workload instance upsert payload.
type InstanceRow struct {
	Environment    string
	InstanceID     string
	RepoID         string
	WorkloadID     string
	WorkloadKind   string
	WorkloadName   string
	Classification string
	Confidence     float64
	Provenance     []string
}

// DeploymentSourceRow is one canonical deployment source edge payload.
type DeploymentSourceRow struct {
	DeploymentRepoID string
	Environment      string
	InstanceID       string
	WorkloadName     string
	Confidence       float64
	Provenance       []string
}

// RuntimePlatformRow is one canonical runtime platform upsert payload.
type RuntimePlatformRow struct {
	Environment      string
	Confidence       float64
	InstanceID       string
	PlatformID       string
	PlatformKind     string
	PlatformLocator  string
	PlatformName     string
	PlatformProvider string
	PlatformRegion   string
	RepoID           string
}

// APIEndpointRow is one canonical Endpoint graph upsert payload.
type APIEndpointRow struct {
	EndpointID   string
	RepoID       string
	WorkloadID   string
	WorkloadName string
	Path         string
	Methods      []string
	OperationIDs []string
	SourceKinds  []string
	SourcePaths  []string
	SpecVersions []string
	APIVersions  []string
}

// RepoDescriptor maps a repository to its inferred workload identity.
type RepoDescriptor struct {
	RepoID     string
	RepoName   string
	WorkloadID string
}

// ProjectionResult holds all outputs from BuildProjectionRows.
type ProjectionResult struct {
	Stats                ProjectionStats
	WorkloadRows         []WorkloadRow
	InstanceRows         []InstanceRow
	DeploymentSourceRows []DeploymentSourceRow
	RuntimePlatformRows  []RuntimePlatformRow
	EndpointRows         []APIEndpointRow
	RepoDescriptors      []RepoDescriptor
}

// InferWorkloadKind infers a workload kind from its name and matched runtime
// resource kinds. Name-based inference takes precedence over resource kinds.
func InferWorkloadKind(name string, resourceKinds []string) string {
	normalized := strings.ToLower(name)
	if strings.Contains(normalized, "cron") {
		return "cronjob"
	}
	if strings.Contains(normalized, "worker") {
		return "worker"
	}
	if strings.Contains(normalized, "consumer") {
		return "consumer"
	}
	if strings.Contains(normalized, "batch") {
		return "batch"
	}
	return "service"
}

// InferWorkloadClassification groups candidates into broad materialization
// classes. Only service and job classifications are allowed to become
// canonical workloads in Wave 1B to avoid weak controller signals creating
// false-positive graph truth.
func InferWorkloadClassification(candidate WorkloadCandidate) string {
	if hasAnyResourceKind(candidate.ResourceKinds, "job", "cronjob") {
		return "job"
	}
	if hasServiceClassificationSignals(candidate) {
		return "service"
	}
	if hasProvenance(candidate.Provenance, "cloudformation_template") {
		return "infrastructure"
	}
	if hasProvenance(candidate.Provenance, "argocd_application", "jenkins_pipeline", "github_actions_workflow") {
		return "utility"
	}
	return "service"
}

func hasServiceClassificationSignals(candidate WorkloadCandidate) bool {
	return hasProvenance(candidate.Provenance,
		"argocd_application_source",
		"argocd_applicationset_deploy_source",
		"kustomize_resource",
		"helm_deployment",
		"dockerfile_runtime",
		"docker_compose_runtime",
	) || hasAnyResourceKind(candidate.ResourceKinds,
		"deployment",
		"service",
		"statefulset",
		"daemonset",
	)
}

// ExtractOverlayEnvironments extracts environment names from repo-relative
// deployment/config paths using bounded family-specific path conventions.
func ExtractOverlayEnvironments(paths []string) []string {
	seen := make(map[string]struct{})
	var environments []string
	for _, raw := range paths {
		for _, pattern := range environmentPathPatterns {
			match := pattern.FindStringSubmatch(raw)
			if match == nil {
				continue
			}
			env := strings.TrimSpace(match[1])
			if env == "" {
				continue
			}
			if _, ok := seen[env]; ok {
				break
			}
			seen[env] = struct{}{}
			environments = append(environments, env)
			break
		}
	}
	return environments
}

// BuildProjectionRows builds batched projection payloads from workload
// candidates. This is the Go equivalent of Python's build_projection_rows.
func BuildProjectionRows(
	candidates []WorkloadCandidate,
	deploymentEnvironments map[string][]string,
) *ProjectionResult {
	return BuildProjectionRowsWithInfrastructurePlatforms(candidates, deploymentEnvironments, nil)
}

// BuildProjectionRowsWithInfrastructurePlatforms builds batched projection
// payloads and augments workload instances with provisioned infrastructure
// platforms when resolved infrastructure evidence is unambiguous.
func BuildProjectionRowsWithInfrastructurePlatforms(
	candidates []WorkloadCandidate,
	deploymentEnvironments map[string][]string,
	infrastructurePlatforms map[string][]InfrastructurePlatformRow,
) *ProjectionResult {
	result := &ProjectionResult{}
	seenWorkloads := make(map[string]struct{})
	seenInstances := make(map[string]struct{})
	seenDeploymentSources := make(map[string]struct{})
	seenRuntimePlatforms := make(map[string]struct{})
	seenEndpoints := make(map[string]int)

	for _, candidate := range candidates {
		if candidate.RepoID == "" || candidate.RepoName == "" {
			continue
		}
		workloadName := candidateWorkloadName(candidate)
		if workloadName == "" {
			continue
		}
		confidence := normalizedCandidateConfidence(candidate.Confidence)
		if confidence < workloadMaterializationMinConfidence {
			continue
		}
		classification := candidate.Classification
		if classification == "" {
			classification = InferWorkloadClassification(candidate)
		}
		if !isMaterializableWorkloadClassification(classification) {
			continue
		}

		workloadID := fmt.Sprintf("workload:%s", workloadName)
		workloadKind := InferWorkloadKind(workloadName, candidate.ResourceKinds)
		provenance := append([]string(nil), candidate.Provenance...)

		result.RepoDescriptors = append(result.RepoDescriptors, RepoDescriptor{
			RepoID:     candidate.RepoID,
			RepoName:   candidate.RepoName,
			WorkloadID: workloadID,
		})

		if _, ok := seenWorkloads[workloadID]; !ok {
			seenWorkloads[workloadID] = struct{}{}
			result.WorkloadRows = append(result.WorkloadRows, WorkloadRow{
				RepoID:         candidate.RepoID,
				WorkloadID:     workloadID,
				WorkloadKind:   workloadKind,
				WorkloadName:   workloadName,
				Classification: classification,
				Confidence:     confidence,
				Provenance:     provenance,
			})
			result.Stats.Workloads++
		}
		addAPIEndpointRows(result, candidate, workloadID, workloadName, seenEndpoints)

		// Resolve environments: deployment overlay environments first (by
		// deployment repo when linked, otherwise source repo), then namespace
		// fallback.
		deploymentRepoIDs := candidateDeploymentRepoIDs(candidate)
		var environments []string
		if len(deploymentRepoIDs) > 0 {
			for _, deploymentRepoID := range deploymentRepoIDs {
				environments = append(environments, deploymentEnvironments[deploymentRepoID]...)
			}
			environments = uniqueSortedStrings(environments)
		} else {
			environments = deploymentEnvironments[candidate.RepoID]
		}
		if len(environments) == 0 {
			for _, ns := range candidate.Namespaces {
				if environment := namespaceEnvironmentFallback(ns); environment != "" {
					environments = append(environments, environment)
				}
			}
		}
		platformKind := inferCandidateRuntimePlatformKind(candidate)

		for _, environment := range environments {
			instanceID := fmt.Sprintf("workload-instance:%s:%s", workloadName, environment)

			if _, ok := seenInstances[instanceID]; !ok {
				seenInstances[instanceID] = struct{}{}
				result.InstanceRows = append(result.InstanceRows, InstanceRow{
					Environment:    environment,
					InstanceID:     instanceID,
					RepoID:         candidate.RepoID,
					WorkloadID:     workloadID,
					WorkloadKind:   workloadKind,
					WorkloadName:   workloadName,
					Classification: classification,
					Confidence:     confidence,
					Provenance:     provenance,
				})
				result.Stats.Instances++
			}

			for _, deploymentRepoID := range deploymentRepoIDs {
				if !deploymentRepoHasEnvironment(deploymentEnvironments, deploymentRepoID, environment) {
					continue
				}
				dsKey := instanceID + "|" + deploymentRepoID
				if _, ok := seenDeploymentSources[dsKey]; ok {
					continue
				}
				seenDeploymentSources[dsKey] = struct{}{}
				result.DeploymentSourceRows = append(result.DeploymentSourceRows, DeploymentSourceRow{
					DeploymentRepoID: deploymentRepoID,
					Environment:      environment,
					InstanceID:       instanceID,
					WorkloadName:     workloadName,
					Confidence:       confidence,
					Provenance:       provenance,
				})
				result.Stats.DeploymentSources++
			}

			if platformKind == "" {
				continue
			}
			platformID := CanonicalPlatformID(CanonicalPlatformInput{
				Kind:        platformKind,
				Name:        environment,
				Environment: environment,
			})
			if platformID == "" {
				continue
			}
			rpKey := instanceID + "|" + platformID
			if _, ok := seenRuntimePlatforms[rpKey]; ok {
				continue
			}
			seenRuntimePlatforms[rpKey] = struct{}{}
			result.RuntimePlatformRows = append(result.RuntimePlatformRows, RuntimePlatformRow{
				Environment:  environment,
				Confidence:   confidence,
				InstanceID:   instanceID,
				PlatformID:   platformID,
				PlatformKind: platformKind,
				PlatformName: environment,
				RepoID:       candidate.RepoID,
			})
		}
		for _, row := range provisionedRuntimePlatformRows(
			candidate,
			workloadName,
			confidence,
			deploymentEnvironments,
			infrastructurePlatforms,
		) {
			if _, ok := seenInstances[row.InstanceID]; !ok {
				seenInstances[row.InstanceID] = struct{}{}
				result.InstanceRows = append(result.InstanceRows, InstanceRow{
					Environment:    row.Environment,
					InstanceID:     row.InstanceID,
					RepoID:         candidate.RepoID,
					WorkloadID:     workloadID,
					WorkloadKind:   workloadKind,
					WorkloadName:   workloadName,
					Classification: classification,
					Confidence:     confidence,
					Provenance:     provenance,
				})
				result.Stats.Instances++
			}
			rpKey := row.InstanceID + "|" + row.PlatformID
			if _, ok := seenRuntimePlatforms[rpKey]; ok {
				continue
			}
			seenRuntimePlatforms[rpKey] = struct{}{}
			result.RuntimePlatformRows = append(result.RuntimePlatformRows, row)
		}
	}

	return result
}

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
	if hasProvenance(candidate.Provenance,
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
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return ""
	}
	switch strings.ToLower(namespace) {
	case "prod", "production", "qa", "stage", "staging", "dev", "development", "test", "sandbox", "preview":
		return namespace
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
