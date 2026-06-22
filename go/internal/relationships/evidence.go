package relationships

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// CatalogEntry maps one repository to its known aliases for matching.
type CatalogEntry struct {
	RepoID  string
	Aliases []string
}

// DiscoverEvidence scans fact envelopes for IaC relationship evidence
// (Terraform, Helm, ArgoCD, Kustomize) and returns discovered evidence facts.
func DiscoverEvidence(envelopes []facts.Envelope, catalog []CatalogEntry) []EvidenceFact {
	if len(envelopes) == 0 {
		return nil
	}

	var evidence []EvidenceFact
	seen := make(map[evidenceKey]struct{})
	contentIndex := buildEvidenceContentIndex(envelopes)
	matcher := newCatalogMatcher(catalog)

	for i := range envelopes {
		discovered := discoverFromEnvelopeWithIndex(envelopes[i], matcher, seen, contentIndex)
		evidence = append(evidence, discovered...)
	}

	return evidence
}

// evidenceKey deduplicates evidence within a single discovery pass.
type evidenceKey struct {
	EvidenceKind   EvidenceKind
	SourceRepoID   string
	TargetRepoID   string
	SourceEntityID string
	TargetEntityID string
	Path           string
	MatchedValue   string
}

// helmChartFilenames are the recognized Helm chart metadata files.
var helmChartFilenames = map[string]struct{}{
	"chart.yaml": {},
	"chart.yml":  {},
}

// kustomizationFilenames are the recognized Kustomize files.
var kustomizationFilenames = map[string]struct{}{
	"kustomization.yaml": {},
	"kustomization.yml":  {},
}

func discoverFromEnvelopeWithIndex(
	envelope facts.Envelope,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
	contentIndex evidenceContentIndex,
) []EvidenceFact {
	if envelope.FactKind == facts.GCPCloudRelationshipFactKind {
		return discoverGCPCloudRelationshipEvidence(envelope, matcher, seen)
	}

	artifactType, _ := envelope.Payload["artifact_type"].(string)
	parsedFileData, _ := envelope.Payload["parsed_file_data"].(map[string]any)
	sourceRepoID, filePath, content := envelopeContentIdentity(envelope)

	if filePath == "" {
		return nil
	}
	if content == "" && len(parsedFileData) == 0 {
		return nil
	}

	var evidence []EvidenceFact

	if len(parsedFileData) > 0 {
		evidence = append(evidence, discoverStructuredTerraformEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen,
		)...)
		evidence = append(evidence, discoverStructuredTerragruntConfigEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen,
		)...)
		evidence = append(evidence, discoverStructuredHelmEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen,
		)...)
		evidence = append(evidence, discoverStructuredArgoCDEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen,
		)...)
	}

	switch {
	case isAnsibleArtifact(artifactType, filePath):
		evidence = append(evidence, discoverAnsibleEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case isTerraformArtifact(artifactType, filePath):
		evidence = append(evidence, discoverTerraformEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case isHelmArtifact(artifactType, filePath):
		evidence = append(evidence, discoverHelmEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case isKustomizeArtifact(filePath):
		evidence = append(evidence, discoverKustomizeEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case isArgoCDArtifact(artifactType, content):
		evidence = append(evidence, discoverArgoCDEvidence(
			sourceRepoID, filePath, content, matcher, seen, contentIndex,
		)...)
	case isJenkinsArtifact(filePath):
		evidence = append(evidence, discoverJenkinsEvidence(
			sourceRepoID, filePath, content, parsedFileData, matcher, seen,
		)...)
	case isDockerfileArtifact(artifactType, filePath):
		evidence = append(evidence, discoverDockerfileEvidence(
			sourceRepoID, filePath, parsedFileData, matcher, seen,
		)...)
	case artifactType == "docker_compose":
		evidence = append(evidence, discoverDockerComposeEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	case artifactType == "github_actions_workflow":
		evidence = append(evidence, discoverGitHubActionsEvidence(
			sourceRepoID, filePath, content, matcher, seen,
		)...)
	}

	return evidence
}

func sourceRepositoryIDFromEnvelope(envelope facts.Envelope) string {
	if repoID, _ := envelope.Payload["repo_id"].(string); strings.TrimSpace(repoID) != "" {
		return strings.TrimSpace(repoID)
	}
	return normalizeRepositoryIdentifier(envelope.ScopeID)
}

func normalizeRepositoryIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, "repository:"); idx > 0 {
		prefix := value[:idx]
		if strings.HasSuffix(prefix, "scope:") {
			return value[idx:]
		}
	}
	return value
}

// discoverHelmEvidence extracts DEPLOYS_FROM evidence from Helm chart content.
func discoverHelmEvidence(
	sourceRepoID, filePath, content string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	lowerName := strings.ToLower(fileBaseName(filePath))
	var evidenceKind EvidenceKind
	var rationale string

	if _, ok := helmChartFilenames[lowerName]; ok {
		evidenceKind = EvidenceKindHelmChart
		rationale = "Helm chart metadata references the target repository"
	} else {
		evidenceKind = EvidenceKindHelmValues
		rationale = "Helm values reference the target repository"
	}
	confidence := DefaultConfidenceRegistry.ConfidenceFor(evidenceKind)

	var evidence []EvidenceFact
	for _, candidate := range extractYAMLStringValues(content) {
		evidence = append(evidence, matchCatalog(
			sourceRepoID, candidate, filePath,
			evidenceKind, RelDeploysFrom, confidence, rationale,
			"helm", matcher, seen, nil,
		)...)
	}

	return evidence
}

// discoverKustomizeEvidence extracts DEPLOYS_FROM evidence from Kustomize overlays.
func discoverKustomizeEvidence(
	sourceRepoID, filePath, content string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, document := range parseYAMLDocuments(content) {
		evidence = append(evidence, discoverKustomizeDocumentEvidence(
			sourceRepoID, filePath, document, matcher, seen,
		)...)
	}
	for _, candidate := range extractYAMLStringValues(content) {
		evidence = append(evidence, matchCatalog(
			sourceRepoID, candidate, filePath,
			EvidenceKindKustomizeResource, RelDeploysFrom, DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindKustomizeResource),
			"Kustomize resources source deployment config from the target repository",
			"kustomize", matcher, seen, nil,
		)...)
	}

	return evidence
}

// discoverArgoCDEvidence extracts ArgoCD Application source references.
func discoverArgoCDEvidence(
	sourceRepoID, filePath, content string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
	contentIndex evidenceContentIndex,
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, document := range parseYAMLDocuments(content) {
		evidence = append(evidence, discoverArgoCDDocumentEvidence(
			sourceRepoID, filePath, document, matcher, seen, contentIndex,
		)...)
	}

	return evidence
}

// extractYAMLStringValues extracts potential string values from YAML content
// using simple pattern matching (not a full YAML parser).
var yamlStringPattern = regexp.MustCompile(`:\s*['"]?([A-Za-z0-9._/-]+)['"]?`)

func extractYAMLStringValues(content string) []string {
	matches := yamlStringPattern.FindAllStringSubmatch(content, -1)
	values := make([]string, 0, len(matches))
	seen := make(map[string]struct{})
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		val := strings.TrimSpace(match[1])
		if val == "" {
			continue
		}
		if _, ok := seen[val]; ok {
			continue
		}
		seen[val] = struct{}{}
		values = append(values, val)
	}
	return values
}

func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

// isHelmArtifact checks if a file is a Helm chart file.
func isHelmArtifact(artifactType, filePath string) bool {
	if artifactType == "helm" {
		return true
	}
	lowerName := strings.ToLower(fileBaseName(filePath))
	if _, ok := helmChartFilenames[lowerName]; ok {
		return true
	}
	return strings.HasPrefix(lowerName, "values") &&
		(strings.HasSuffix(lowerName, ".yaml") || strings.HasSuffix(lowerName, ".yml"))
}

// isKustomizeArtifact checks if a file is a Kustomize file.
func isKustomizeArtifact(filePath string) bool {
	lowerName := strings.ToLower(fileBaseName(filePath))
	_, ok := kustomizationFilenames[lowerName]
	return ok
}

// isArgoCDArtifact checks if content appears to be an ArgoCD Application spec.
func isArgoCDArtifact(artifactType, content string) bool {
	if artifactType == "argocd" {
		return true
	}
	return strings.Contains(content, "kind: Application") ||
		strings.Contains(content, "kind: ApplicationSet")
}

func isDockerfileArtifact(artifactType, filePath string) bool {
	if strings.EqualFold(artifactType, "dockerfile") {
		return true
	}
	lowerName := strings.ToLower(fileBaseName(filePath))
	return lowerName == "dockerfile" || strings.HasPrefix(lowerName, "dockerfile.")
}

func isJenkinsArtifact(filePath string) bool {
	base := strings.ToLower(fileBaseName(filePath))
	return base == "jenkinsfile" || strings.HasPrefix(base, "jenkinsfile.")
}

// fileBaseName returns the last path component of a file path.
func fileBaseName(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}
