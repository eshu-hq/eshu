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

	for i := range envelopes {
		discovered := discoverFromEnvelopeWithIndex(envelopes[i], catalog, seen, contentIndex)
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

// terraformPattern describes one regex-based Terraform evidence extractor.
type terraformPattern struct {
	EvidenceKind     EvidenceKind
	RelationshipType RelationshipType
	Pattern          *regexp.Regexp
	Confidence       float64
	Rationale        string
}

var terraformPatterns = []terraformPattern{
	{
		EvidenceKind:     EvidenceKindTerraformAppRepo,
		RelationshipType: RelProvisionsDependencyFor,
		Pattern:          regexp.MustCompile(`(?i)\bapp_repo\b\s*=\s*"([^"]+)"`),
		Confidence:       0.99,
		Rationale:        "Terraform app_repo points at the target repository",
	},
	{
		EvidenceKind:     EvidenceKindTerraformAppName,
		RelationshipType: RelProvisionsDependencyFor,
		Pattern:          regexp.MustCompile(`(?i)\bapp_name\b\s*=\s*"([^"]+)"`),
		Confidence:       0.94,
		Rationale:        "Terraform app_name matches the target repository name",
	},
	{
		EvidenceKind:     EvidenceKindTerraformGitHubRepo,
		RelationshipType: RelProvisionsDependencyFor,
		Pattern:          regexp.MustCompile(`(?i)github\.com[:/][^/"'\s]+/([A-Za-z0-9._-]+)(?:\.git)?`),
		Confidence:       0.98,
		Rationale:        "Terraform GitHub reference points at the target repository",
	},
	{
		EvidenceKind:     EvidenceKindTerraformGitHubActions,
		RelationshipType: RelProvisionsDependencyFor,
		Pattern:          regexp.MustCompile(`(?i)repo:[^/:\s]+/([A-Za-z0-9._-]+):`),
		Confidence:       0.97,
		Rationale:        "Terraform GitHub Actions subject references the target repository",
	},
	{
		EvidenceKind:     EvidenceKindTerraformConfigPath,
		RelationshipType: RelProvisionsDependencyFor,
		Pattern:          regexp.MustCompile(`(?i)/(?:configd|api)/([A-Za-z0-9._-]+)/`),
		Confidence:       0.90,
		Rationale:        "Terraform configuration path references the target repository name",
	},
}

var (
	terraformModuleBlockPattern    = regexp.MustCompile(`(?is)module\s+"([^"]+)"\s*\{(.*?)\}`)
	terragruntConfigPathPattern    = regexp.MustCompile(`(?i)\bconfig_path\s*=\s*"([^"]+)"`)
	terraformSourcePattern         = regexp.MustCompile(`(?i)\bsource\b\s*=\s*"([^"]+)"`)
	terraformRegistrySourcePattern = regexp.MustCompile(`^[a-z0-9._-]+/[a-z0-9._-]+/[a-z0-9._-]+(?://.*)?$`)
	evidenceQuotedStringPattern    = regexp.MustCompile(`"([^"]+)"`)
)

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
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
	contentIndex evidenceContentIndex,
) []EvidenceFact {
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
			sourceRepoID, filePath, parsedFileData, catalog, seen,
		)...)
		evidence = append(evidence, discoverStructuredTerragruntConfigEvidence(
			sourceRepoID, filePath, parsedFileData, catalog, seen,
		)...)
		evidence = append(evidence, discoverStructuredHelmEvidence(
			sourceRepoID, filePath, parsedFileData, catalog, seen,
		)...)
		evidence = append(evidence, discoverStructuredArgoCDEvidence(
			sourceRepoID, filePath, parsedFileData, catalog, seen,
		)...)
	}

	switch {
	case isAnsibleArtifact(artifactType, filePath):
		evidence = append(evidence, discoverAnsibleEvidence(
			sourceRepoID, filePath, content, catalog, seen,
		)...)
	case isTerraformArtifact(artifactType, filePath):
		evidence = append(evidence, discoverTerraformEvidence(
			sourceRepoID, filePath, content, catalog, seen,
		)...)
	case isHelmArtifact(artifactType, filePath):
		evidence = append(evidence, discoverHelmEvidence(
			sourceRepoID, filePath, content, catalog, seen,
		)...)
	case isKustomizeArtifact(filePath):
		evidence = append(evidence, discoverKustomizeEvidence(
			sourceRepoID, filePath, content, catalog, seen,
		)...)
	case isArgoCDArtifact(artifactType, content):
		evidence = append(evidence, discoverArgoCDEvidence(
			sourceRepoID, filePath, content, catalog, seen, contentIndex,
		)...)
	case isJenkinsArtifact(filePath):
		evidence = append(evidence, discoverJenkinsEvidence(
			sourceRepoID, filePath, content, parsedFileData, catalog, seen,
		)...)
	case isDockerfileArtifact(artifactType, filePath):
		evidence = append(evidence, discoverDockerfileEvidence(
			sourceRepoID, filePath, parsedFileData, catalog, seen,
		)...)
	case artifactType == "docker_compose":
		evidence = append(evidence, discoverDockerComposeEvidence(
			sourceRepoID, filePath, content, catalog, seen,
		)...)
	case artifactType == "github_actions_workflow":
		evidence = append(evidence, discoverGitHubActionsEvidence(
			sourceRepoID, filePath, content, catalog, seen,
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

func normalizeTerraformEvidencePathExpression(expression string) string {
	trimmed := strings.TrimSpace(expression)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, ",")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return ""
	}
	if !strings.ContainsAny(trimmed, "()[]{}\"") {
		if value := normalizeTerraformEvidencePathLiteral(trimmed, trimmed); value != "" {
			return value
		}
	}
	matches := evidenceQuotedStringPattern.FindAllStringSubmatch(trimmed, -1)
	for index := len(matches) - 1; index >= 0; index-- {
		match := matches[index]
		if len(match) < 2 {
			continue
		}
		if value := normalizeTerraformEvidencePathLiteral(match[1], trimmed); value != "" {
			return value
		}
	}
	return ""
}

func normalizeTerraformEvidencePathLiteral(value, expression string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(trimmed, "://") ||
		strings.HasPrefix(lower, "git::") ||
		strings.HasPrefix(lower, "tfr:///") {
		return ""
	}

	replacer := strings.NewReplacer(
		"${get_repo_root()}/", "",
		"${path.module}/", "",
		"${path_relative_to_include()}/", "",
		"${path_relative_to_include()}", "",
		"${get_parent_terragrunt_dir()}/", "",
		"${get_terragrunt_dir()}/", "",
	)
	trimmed = replacer.Replace(trimmed)
	trimmed = strings.TrimPrefix(trimmed, "./")
	trimmed = strings.TrimPrefix(trimmed, "/")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" || trimmed == "." {
		return ""
	}

	if strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	if strings.Contains(expression, "get_repo_root(") ||
		strings.Contains(expression, "path.module") ||
		strings.Contains(expression, "get_parent_terragrunt_dir(") ||
		strings.Contains(expression, "get_terragrunt_dir(") ||
		strings.Contains(expression, "path_relative_to_include(") ||
		strings.Contains(expression, "local.") ||
		strings.Contains(expression, "join(") ||
		strings.Contains(expression, "lookup(") ||
		strings.Contains(expression, "file(") ||
		strings.Contains(expression, "templatefile(") {
		return trimmed
	}
	return ""
}

// discoverTerraformEvidence applies Terraform regex patterns against file content.
func discoverTerraformEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	evidence = append(evidence, discoverTerraformModuleSourceEvidence(
		sourceRepoID, filePath, content, catalog, seen,
	)...)
	evidence = append(evidence, discoverTerraformRuntimeServiceModuleEvidence(
		sourceRepoID, filePath, content, catalog, seen,
	)...)
	evidence = append(evidence, discoverTerragruntDependencyConfigPathEvidence(
		sourceRepoID, filePath, content, catalog, seen,
	)...)
	iamConfigReads := terraformIAMSSMConfigReadCandidates(content)
	evidence = append(evidence, discoverTerraformIAMSSMConfigReadEvidence(
		sourceRepoID, filePath, iamConfigReads, catalog, seen,
	)...)

	for _, tp := range terraformPatterns {
		matches := tp.Pattern.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			candidate := strings.TrimSpace(match[1])
			if tp.EvidenceKind == EvidenceKindTerraformConfigPath && isTerraformIAMConfigReadCandidate(candidate, iamConfigReads) {
				continue
			}
			evidence = append(evidence, matchCatalog(
				sourceRepoID, candidate, filePath,
				tp.EvidenceKind, tp.RelationshipType, tp.Confidence, tp.Rationale,
				"terraform", catalog, seen, nil,
			)...)
		}
	}

	evidence = append(evidence, discoverTerraformSchemaEvidence(
		sourceRepoID, filePath, content, catalog, seen,
	)...)

	return evidence
}

func discoverStructuredTerraformEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	if modules, ok := parsedFileData["terraform_modules"].([]any); ok {
		for _, item := range modules {
			module, ok := item.(map[string]any)
			if !ok {
				continue
			}
			source := strings.TrimSpace(payloadString(module, "source"))
			helperDerived := false
			if normalized := normalizeTerraformEvidencePathExpression(source); normalized != "" {
				source = normalized
				helperDerived = normalized != strings.TrimSpace(payloadString(module, "source"))
			}
			if source == "" {
				continue
			}
			if !helperDerived && !looksLikeRemoteModuleSource(source) {
				continue
			}
			evidence = append(evidence, matchCatalog(
				sourceRepoID,
				source,
				filePath,
				EvidenceKindTerraformModuleSource,
				RelUsesModule,
				0.98,
				"Terraform or Terragrunt module source points at the target module repository",
				"terraform-module-source",
				catalog,
				seen,
				withFirstPartyRefDetails(
					map[string]any{
						"module_name": module["name"],
						"source_ref":  source,
					},
					"terraform_module_source",
					payloadString(module, "name"),
					"",
					"",
					"",
					normalizeTerraformFirstPartyRef(source),
				),
			)...)
		}
	}

	if dependencies, ok := parsedFileData["terragrunt_dependencies"].([]any); ok {
		for _, item := range dependencies {
			dependency, ok := item.(map[string]any)
			if !ok {
				continue
			}
			configPath := strings.TrimSpace(payloadString(dependency, "config_path"))
			helperDerived := false
			if normalized := normalizeTerraformEvidencePathExpression(configPath); normalized != "" {
				configPath = normalized
				helperDerived = normalized != strings.TrimSpace(payloadString(dependency, "config_path"))
			}
			if configPath == "" {
				continue
			}
			if !helperDerived && !looksLikeRemoteModuleSource(configPath) {
				continue
			}
			evidence = append(evidence, matchCatalog(
				sourceRepoID,
				configPath,
				filePath,
				EvidenceKindTerragruntDependencyConfigPath,
				RelDiscoversConfigIn,
				0.90,
				"Terragrunt dependency config_path points at the target repository",
				"terragrunt-dependency-config-path",
				catalog,
				seen,
				withFirstPartyRefDetails(
					map[string]any{
						"dependency_name": dependency["name"],
						"config_path":     configPath,
					},
					"terragrunt_dependency_config_path",
					payloadString(dependency, "name"),
					configPath,
					"",
					"",
					normalizeTerraformFirstPartyRef(configPath),
				),
			)...)
		}
	}

	return evidence
}

// discoverHelmEvidence extracts DEPLOYS_FROM evidence from Helm chart content.
func discoverHelmEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	lowerName := strings.ToLower(fileBaseName(filePath))
	var evidenceKind EvidenceKind
	var confidence float64
	var rationale string

	if _, ok := helmChartFilenames[lowerName]; ok {
		evidenceKind = EvidenceKindHelmChart
		confidence = 0.90
		rationale = "Helm chart metadata references the target repository"
	} else {
		evidenceKind = EvidenceKindHelmValues
		confidence = 0.84
		rationale = "Helm values reference the target repository"
	}

	var evidence []EvidenceFact
	for _, candidate := range extractYAMLStringValues(content) {
		evidence = append(evidence, matchCatalog(
			sourceRepoID, candidate, filePath,
			evidenceKind, RelDeploysFrom, confidence, rationale,
			"helm", catalog, seen, nil,
		)...)
	}

	return evidence
}

// discoverKustomizeEvidence extracts DEPLOYS_FROM evidence from Kustomize overlays.
func discoverKustomizeEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, document := range parseYAMLDocuments(content) {
		evidence = append(evidence, discoverKustomizeDocumentEvidence(
			sourceRepoID, filePath, document, catalog, seen,
		)...)
	}
	for _, candidate := range extractYAMLStringValues(content) {
		evidence = append(evidence, matchCatalog(
			sourceRepoID, candidate, filePath,
			EvidenceKindKustomizeResource, RelDeploysFrom, 0.90,
			"Kustomize resources source deployment config from the target repository",
			"kustomize", catalog, seen, nil,
		)...)
	}

	return evidence
}

// discoverArgoCDEvidence extracts ArgoCD Application source references.
func discoverArgoCDEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
	contentIndex evidenceContentIndex,
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, document := range parseYAMLDocuments(content) {
		evidence = append(evidence, discoverArgoCDDocumentEvidence(
			sourceRepoID, filePath, document, catalog, seen, contentIndex,
		)...)
	}

	return evidence
}

// matchCatalog matches a candidate string against catalog entries and returns
// evidence facts for each match.
func matchCatalog(
	sourceRepoID, candidate, filePath string,
	evidenceKind EvidenceKind,
	relType RelationshipType,
	confidence float64,
	rationale, extractor string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
	extraDetails map[string]any,
) []EvidenceFact {
	var evidence []EvidenceFact

	for _, entry := range catalog {
		if entry.RepoID == sourceRepoID {
			continue
		}
		matchedAlias := matchesEntry(candidate, entry)
		if matchedAlias == "" {
			continue
		}
		key := evidenceKey{
			EvidenceKind:   evidenceKind,
			SourceRepoID:   sourceRepoID,
			TargetRepoID:   entry.RepoID,
			SourceEntityID: "",
			TargetEntityID: "",
			Path:           filePath,
			MatchedValue:   candidate,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		details := map[string]any{
			"path":          filePath,
			"matched_alias": matchedAlias,
			"matched_value": candidate,
			"extractor":     extractor,
		}
		for key, value := range extraDetails {
			details[key] = value
		}
		evidence = append(evidence, EvidenceFact{
			EvidenceKind:     evidenceKind,
			RelationshipType: relType,
			SourceRepoID:     sourceRepoID,
			TargetRepoID:     entry.RepoID,
			Confidence:       confidence,
			Rationale:        rationale,
			Details:          details,
		})
	}

	return evidence
}

// matchesEntry checks if a candidate string matches any alias of a catalog entry.
// Returns the matched alias or empty string.
func matchesEntry(candidate string, entry CatalogEntry) string {
	lowerCandidate := strings.ToLower(strings.TrimSpace(candidate))
	for _, alias := range entry.Aliases {
		lowerAlias := strings.ToLower(strings.TrimSpace(alias))
		if lowerAlias == "" {
			continue
		}
		if strings.Contains(lowerCandidate, lowerAlias) ||
			matchesPrivateTerraformRegistryAlias(lowerCandidate, lowerAlias) {
			return alias
		}
	}
	return ""
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

// isTerraformArtifact checks if a file is a Terraform/Terragrunt file.
func isTerraformArtifact(artifactType, filePath string) bool {
	if artifactType == "terraform" || artifactType == "terraform_hcl" || artifactType == "terragrunt" {
		return true
	}
	lower := strings.ToLower(filePath)
	return strings.HasSuffix(lower, ".tf") ||
		strings.HasSuffix(lower, ".tf.json") ||
		strings.HasSuffix(lower, ".tfvars") ||
		strings.HasSuffix(lower, ".tfvars.json") ||
		strings.HasSuffix(lower, ".hcl")
}

func discoverTerraformModuleSourceEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	for _, candidate := range extractRemoteTerraformModuleSources(filePath, content) {
		evidence = append(evidence, matchCatalog(
			sourceRepoID,
			candidate,
			filePath,
			EvidenceKindTerraformModuleSource,
			RelUsesModule,
			0.98,
			"Terraform or Terragrunt module source points at the target module repository",
			"terraform-module-source",
			catalog,
			seen,
			map[string]any{"source_ref": candidate},
		)...)
	}

	return evidence
}

func discoverTerragruntDependencyConfigPathEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	if !strings.EqualFold(fileBaseName(filePath), "terragrunt.hcl") {
		return nil
	}

	var evidence []EvidenceFact
	for _, configPath := range extractTerragruntConfigPaths(content) {
		if !looksLikeRemoteModuleSource(configPath) {
			continue
		}
		evidence = append(evidence, matchCatalog(
			sourceRepoID,
			configPath,
			filePath,
			EvidenceKindTerragruntDependencyConfigPath,
			RelDiscoversConfigIn,
			0.90,
			"Terragrunt dependency config_path points at the target repository",
			"terragrunt-dependency-config-path",
			catalog,
			seen,
			map[string]any{"config_path": configPath},
		)...)
	}

	return evidence
}

func extractRemoteTerraformModuleSources(filePath, content string) []string {
	var matches []string
	seen := make(map[string]struct{})
	add := func(source string) {
		source = strings.TrimSpace(source)
		if source == "" || !looksLikeRemoteModuleSource(source) {
			return
		}
		if _, ok := seen[source]; ok {
			return
		}
		seen[source] = struct{}{}
		matches = append(matches, source)
	}

	if strings.EqualFold(fileBaseName(filePath), "terragrunt.hcl") {
		for _, source := range extractSourceAssignments(content) {
			add(source)
		}
		return matches
	}

	for _, block := range terraformModuleBlockPattern.FindAllStringSubmatch(content, -1) {
		if len(block) < 3 {
			continue
		}
		for _, source := range extractSourceAssignments(block[2]) {
			add(source)
		}
	}

	return matches
}

func extractSourceAssignments(body string) []string {
	raw := terraformSourcePattern.FindAllStringSubmatch(body, -1)
	values := make([]string, 0, len(raw))
	for _, match := range raw {
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func extractTerragruntConfigPaths(body string) []string {
	raw := terragruntConfigPathPattern.FindAllStringSubmatch(body, -1)
	values := make([]string, 0, len(raw))
	for _, match := range raw {
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func looksLikeRemoteModuleSource(source string) bool {
	lower := normalizeTerraformModuleSource(source)
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "tfr:///") {
		return false
	}
	if isPrivateTerraformRegistryModuleSource(lower) {
		return true
	}
	if terraformRegistrySourcePattern.MatchString(lower) {
		return false
	}
	if strings.HasPrefix(lower, "./") || strings.HasPrefix(lower, "../") || strings.HasPrefix(lower, "/") {
		return true
	}
	return strings.Contains(lower, "github.com") ||
		strings.Contains(lower, "git::") ||
		strings.HasPrefix(lower, "git@") ||
		strings.HasPrefix(lower, "ssh://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "http://")
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
