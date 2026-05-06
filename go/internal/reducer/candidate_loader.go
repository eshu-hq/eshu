package reducer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// repoSignals aggregates K8s and ArgoCD signal data during candidate
// extraction from fact envelopes.
type repoSignals struct {
	resourceKinds map[string]struct{}
	namespaces    map[string]struct{}
	hasArgoCD     bool
	confidence    float64
	provenance    []string
}

// ExtractWorkloadCandidates builds workload candidates and deployment overlay
// environments from fact envelopes for one scope generation. It examines
// repository facts for repo identity and file facts for K8s resources and
// ArgoCD applications to determine which repositories define deployable
// workloads.
//
// Returns (candidates, deploymentEnvironmentsByRepoID).
func ExtractWorkloadCandidates(envelopes []facts.Envelope) ([]WorkloadCandidate, map[string][]string) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	// Pass 1: collect repo identity from the repository fact.
	repos := make(map[string]string) // repo_id → repo_name
	for _, env := range envelopes {
		if env.FactKind != "repository" {
			continue
		}
		repoID := payloadStr(env.Payload, "graph_id")
		repoName := payloadStr(env.Payload, "name")
		if repoID != "" && repoName != "" {
			repos[repoID] = repoName
		}
	}

	// Pass 2: scan file facts for K8s resources and ArgoCD apps.
	signals := make(map[string]*repoSignals)
	deploymentEnvs := make(map[string]map[string]struct{})
	apiEndpoints := extractAPIEndpointSignals(envelopes)

	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}
		repoID := payloadStr(env.Payload, "repo_id")
		if repoID == "" {
			continue
		}

		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		relativePath := payloadStr(env.Payload, "relative_path")

		sig := signals[repoID]
		if sig == nil {
			sig = &repoSignals{
				resourceKinds: make(map[string]struct{}),
				namespaces:    make(map[string]struct{}),
			}
			signals[repoID] = sig
		}

		extractK8sSignals(fileData, sig)
		extractArgoCDSignals(fileData, sig)
		extractArtifactSignals(payloadStr(env.Payload, "language"), relativePath, fileData, sig)
		extractOverlayEnvs(repoID, relativePath, deploymentEnvs)
	}

	// Pass 3: build candidates for repos that have workload signals.
	var candidates []WorkloadCandidate
	for repoID, repoName := range repos {
		sig := signals[repoID]
		if sig == nil {
			continue
		}
		if len(sig.resourceKinds) == 0 && !sig.hasArgoCD && len(sig.provenance) == 0 {
			continue
		}

		candidate := WorkloadCandidate{
			RepoID:        repoID,
			RepoName:      repoName,
			ResourceKinds: sortedKeys(sig.resourceKinds),
			Namespaces:    sortedKeys(sig.namespaces),
			Confidence:    sig.confidence,
			Provenance:    append([]string(nil), sig.provenance...),
			APIEndpoints:  append([]APIEndpointSignal(nil), apiEndpoints[repoID]...),
		}
		candidate.Classification = InferWorkloadClassification(candidate)
		candidates = append(candidates, candidate)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].RepoName < candidates[j].RepoName
	})

	envResult := make(map[string][]string, len(deploymentEnvs))
	for repoID, envSet := range deploymentEnvs {
		envResult[repoID] = sortedKeys(envSet)
	}

	return candidates, envResult
}

// ExtractOverlayEnvironmentsFromEnvelopes extracts deployment overlay
// environments from file fact envelopes without building full workload
// candidates. Used for cross-repo environment loading where only the overlay
// paths matter.
func ExtractOverlayEnvironmentsFromEnvelopes(envelopes []facts.Envelope) map[string][]string {
	deploymentEnvs := make(map[string]map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != "file" {
			continue
		}
		repoID := payloadStr(env.Payload, "repo_id")
		if repoID == "" {
			continue
		}
		relativePath := payloadStr(env.Payload, "relative_path")
		extractOverlayEnvs(repoID, relativePath, deploymentEnvs)
	}

	result := make(map[string][]string, len(deploymentEnvs))
	for repoID, envSet := range deploymentEnvs {
		result[repoID] = sortedKeys(envSet)
	}
	return result
}

func extractK8sSignals(fileData map[string]any, sig *repoSignals) {
	resources, ok := fileData["k8s_resources"].([]any)
	if !ok {
		return
	}
	for _, item := range resources {
		resource, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(fmt.Sprint(resource["kind"])))
		if kind != "" && kind != "<nil>" {
			sig.resourceKinds[kind] = struct{}{}
		}
		ns := strings.TrimSpace(fmt.Sprint(resource["namespace"]))
		if ns != "" && ns != "<nil>" {
			sig.namespaces[ns] = struct{}{}
		}
	}
	if len(resources) > 0 {
		sig.addProvenance("k8s_resource", 0.98)
	}
}

func extractArgoCDSignals(fileData map[string]any, sig *repoSignals) {
	for _, bucket := range []string{"argocd_applications", "argocd_applicationsets"} {
		apps, ok := fileData[bucket].([]any)
		if !ok {
			continue
		}
		if len(apps) > 0 {
			sig.hasArgoCD = true
			sig.addProvenance("argocd_application", 0.95)
		}
	}
}

func extractArtifactSignals(
	language, relativePath string,
	fileData map[string]any,
	sig *repoSignals,
) {
	lang := strings.ToLower(strings.TrimSpace(language))

	// Helm chart: Chart.yaml with chart_type indicates a packaged deployment.
	if chartType := strings.TrimSpace(fmt.Sprint(fileData["chart_type"])); chartType != "" && chartType != "<nil>" {
		sig.addProvenance("helm_chart", 0.92)
		return
	}

	// Dockerfile: language is "dockerfile" and has parsed stages.
	if lang == "dockerfile" && len(sliceValue(fileData["dockerfile_stages"])) > 0 {
		sig.addProvenance("dockerfile_runtime", 0.88)
		if relativePath != "" {
			sig.addProvenance("dockerfile_runtime:"+relativePath, 0.88)
		}
		return
	}

	// Jenkins: language is "groovy" or path is Jenkinsfile, with pipeline signals.
	if lang == "groovy" || strings.EqualFold(strings.TrimSpace(relativePath), "Jenkinsfile") {
		if isJenkinsArtifact(relativePath, fileData) {
			sig.addProvenance("jenkins_pipeline", 0.42)
			return
		}
	}

	// Docker Compose: detected via parsed docker_compose_services or service_name keys.
	if len(sliceValue(fileData["docker_compose_services"])) > 0 {
		sig.addProvenance("docker_compose_runtime", 0.78)
		return
	}

	// CloudFormation: detected via parsed cloudformation_resources etc.
	if hasCloudFormationSignals(fileData) {
		sig.addProvenance("cloudformation_template", 0.58)
		return
	}

	// GitHub Actions workflow: yaml file under .github/workflows/.
	if lang == "yaml" && strings.HasPrefix(relativePath, ".github/workflows/") {
		if strings.TrimSpace(fmt.Sprint(fileData["artifact_type"])) == "github_actions_workflow" ||
			len(sliceValue(fileData["github_actions_workflow_triggers"])) > 0 ||
			len(sliceValue(fileData["github_actions_reusable_workflow_refs"])) > 0 {
			sig.addProvenance("github_actions_workflow", 0.45)
		}
	}
}

func hasCloudFormationSignals(fileData map[string]any) bool {
	for _, key := range []string{
		"cloudformation_resources",
		"cloudformation_parameters",
		"cloudformation_outputs",
		"cloudformation_cross_stack_imports",
		"cloudformation_cross_stack_exports",
	} {
		if len(sliceValue(fileData[key])) > 0 {
			return true
		}
	}
	return false
}

func isJenkinsArtifact(relativePath string, fileData map[string]any) bool {
	if strings.EqualFold(strings.TrimSpace(relativePath), "Jenkinsfile") {
		return true
	}
	return len(sliceValue(fileData["jenkins_pipeline_calls"])) > 0 ||
		len(sliceValue(fileData["pipeline_calls"])) > 0
}

func extractOverlayEnvs(repoID, relativePath string, deploymentEnvs map[string]map[string]struct{}) {
	if relativePath == "" {
		return
	}
	environments := ExtractOverlayEnvironments([]string{relativePath})
	if len(environments) == 0 {
		return
	}
	envSet := deploymentEnvs[repoID]
	if envSet == nil {
		envSet = make(map[string]struct{})
		deploymentEnvs[repoID] = envSet
	}
	for _, env := range environments {
		envSet[env] = struct{}{}
	}
}

func payloadStr(payload map[string]any, key string) string {
	val, ok := payload[key]
	if !ok {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(val))
	if s == "<nil>" {
		return ""
	}
	return s
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sliceValue(value any) []any {
	items, _ := value.([]any)
	return items
}

func (s *repoSignals) addProvenance(kind string, confidence float64) {
	if s == nil || kind == "" {
		return
	}
	for _, existing := range s.provenance {
		if existing == kind {
			if confidence > s.confidence {
				s.confidence = confidence
			}
			return
		}
	}
	s.provenance = append(s.provenance, kind)
	if confidence > s.confidence {
		s.confidence = confidence
	}
}
