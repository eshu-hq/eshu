// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

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
		Confidence:       DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerraformAppRepo),
		Rationale:        "Terraform app_repo points at the target repository",
	},
	{
		EvidenceKind:     EvidenceKindTerraformAppName,
		RelationshipType: RelProvisionsDependencyFor,
		Pattern:          regexp.MustCompile(`(?i)\bapp_name\b\s*=\s*"([^"]+)"`),
		Confidence:       DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerraformAppName),
		Rationale:        "Terraform app_name matches the target repository name",
	},
	{
		EvidenceKind:     EvidenceKindTerraformGitHubRepo,
		RelationshipType: RelProvisionsDependencyFor,
		Pattern:          regexp.MustCompile(`(?i)github\.com[:/][^/"'\s]+/([A-Za-z0-9._-]+)(?:\.git)?`),
		Confidence:       DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerraformGitHubRepo),
		Rationale:        "Terraform GitHub reference points at the target repository",
	},
	{
		EvidenceKind:     EvidenceKindTerraformGitHubActions,
		RelationshipType: RelProvisionsDependencyFor,
		Pattern:          regexp.MustCompile(`(?i)repo:[^/:\s]+/([A-Za-z0-9._-]+):`),
		Confidence:       DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerraformGitHubActions),
		Rationale:        "Terraform GitHub Actions subject references the target repository",
	},
	{
		EvidenceKind:     EvidenceKindTerraformConfigPath,
		RelationshipType: RelProvisionsDependencyFor,
		Pattern:          regexp.MustCompile(`(?i)/(?:configd|api)/([A-Za-z0-9._-]+)/`),
		Confidence:       DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerraformConfigPath),
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
// discoverTerraformEvidence applies Terraform regex patterns against file content
// and captures real byte-level citation (start_line, end_line, byte_offset,
// byte_length) for each match alongside the commit_sha forwarded from the
// envelope. Sub-helpers that do not produce regex match indices (module source,
// schema) degrade safely: they omit line/byte offsets but still forward
// commit_sha so Canonical() gets at least a version pin.
func discoverTerraformEvidence(
	sourceRepoID, filePath, content, commitSHA string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	evidence = append(evidence, discoverTerraformModuleSourceEvidence(
		sourceRepoID, filePath, content, commitSHA, matcher, seen,
	)...)
	evidence = append(evidence, discoverTerraformRuntimeServiceModuleEvidence(
		sourceRepoID, filePath, content, matcher, seen,
	)...)
	evidence = append(evidence, discoverTerragruntDependencyConfigPathEvidence(
		sourceRepoID, filePath, content, matcher, seen,
	)...)
	iamConfigReads := terraformIAMSSMConfigReadCandidates(content)
	evidence = append(evidence, discoverTerraformIAMSSMConfigReadEvidence(
		sourceRepoID, filePath, iamConfigReads, matcher, seen,
	)...)

	for _, tp := range terraformPatterns {
		// FindAllStringIndex returns [start, end) byte pairs for the full
		// match; we pair each index entry with the submatch to get the
		// capture group value without a second regex pass.
		allIdx := tp.Pattern.FindAllStringIndex(content, -1)
		allMatch := tp.Pattern.FindAllStringSubmatch(content, -1)
		for i, match := range allMatch {
			if len(match) < 2 {
				continue
			}
			candidate := strings.TrimSpace(match[1])
			if tp.EvidenceKind == EvidenceKindTerraformConfigPath && isTerraformIAMConfigReadCandidate(candidate, iamConfigReads) {
				continue
			}
			var extra map[string]any
			if i < len(allIdx) {
				loc := allIdx[i]
				extra = byteCitation(content, loc[0], loc[1])
			}
			extra = mergeCommitSHA(extra, commitSHA)
			evidence = append(evidence, matchCatalog(
				sourceRepoID, candidate, filePath,
				tp.EvidenceKind, tp.RelationshipType, tp.Confidence, tp.Rationale,
				"terraform", matcher, seen, extra,
			)...)
		}
	}

	evidence = append(evidence, discoverTerraformSchemaEvidence(
		sourceRepoID, filePath, content, matcher, seen,
	)...)

	return evidence
}

// discoverStructuredTerraformEvidence reads the parsed_file_data
// terraform_modules and terragrunt_dependencies inner keys through the
// typed factschema.DecodeParsedFileDataTerraformModules /
// DecodeParsedFileDataTerragruntDependencies accessors (issue #5445 slice
// 1) rather than a raw map lookup. Both accessors skip a malformed row
// rather than failing the whole bucket, so a decode error here is always
// nil in practice; the error return is ignored deliberately, matching the
// pre-typing raw-map read's silent tolerance of an absent/wrong-shape
// bucket.
func discoverStructuredTerraformEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	modules, _ := factschema.DecodeParsedFileDataTerraformModules(parsedFileData)
	for _, module := range modules {
		source := strings.TrimSpace(module.Source)
		helperDerived := false
		if normalized := normalizeTerraformEvidencePathExpression(source); normalized != "" {
			source = normalized
			helperDerived = normalized != strings.TrimSpace(module.Source)
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
			DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerraformModuleSource),
			"Terraform or Terragrunt module source points at the target module repository",
			"terraform-module-source",
			matcher,
			seen,
			withFirstPartyRefDetails(
				map[string]any{
					"module_name": module.Name,
					"source_ref":  source,
				},
				"terraform_module_source",
				module.Name,
				"",
				"",
				"",
				normalizeTerraformFirstPartyRef(source),
			),
		)...)
	}

	dependencies, _ := factschema.DecodeParsedFileDataTerragruntDependencies(parsedFileData)
	for _, dependency := range dependencies {
		configPath := strings.TrimSpace(dependency.ConfigPath)
		helperDerived := false
		if normalized := normalizeTerraformEvidencePathExpression(configPath); normalized != "" {
			configPath = normalized
			helperDerived = normalized != strings.TrimSpace(dependency.ConfigPath)
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
			DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerragruntDependencyConfigPath),
			"Terragrunt dependency config_path points at the target repository",
			"terragrunt-dependency-config-path",
			matcher,
			seen,
			withFirstPartyRefDetails(
				map[string]any{
					"dependency_name": dependency.Name,
					"config_path":     configPath,
				},
				"terragrunt_dependency_config_path",
				dependency.Name,
				configPath,
				"",
				"",
				normalizeTerraformFirstPartyRef(configPath),
			),
		)...)
	}

	return evidence
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
	sourceRepoID, filePath, content, commitSHA string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	for _, candidate := range extractRemoteTerraformModuleSources(filePath, content) {
		extra := mergeCommitSHA(map[string]any{"source_ref": candidate}, commitSHA)
		evidence = append(evidence, matchCatalog(
			sourceRepoID,
			candidate,
			filePath,
			EvidenceKindTerraformModuleSource,
			RelUsesModule,
			DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerraformModuleSource),
			"Terraform or Terragrunt module source points at the target module repository",
			"terraform-module-source",
			matcher,
			seen,
			extra,
		)...)
	}

	return evidence
}

func discoverTerragruntDependencyConfigPathEvidence(
	sourceRepoID, filePath, content string,
	matcher *catalogMatcher,
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
			DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerragruntDependencyConfigPath),
			"Terragrunt dependency config_path points at the target repository",
			"terragrunt-dependency-config-path",
			matcher,
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
