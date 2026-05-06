package relationships

import (
	"regexp"
	"strings"
)

type terraformRuntimeServiceModuleFamily struct {
	kind     string
	patterns []string
}

var (
	terraformRuntimeServiceModuleFamilies = []terraformRuntimeServiceModuleFamily{
		{kind: "ecs", patterns: []string{"ecs-application/aws"}},
		{kind: "lambda", patterns: []string{"lambda-function", "serverless-function"}},
		{kind: "cloudflare_workers", patterns: []string{"cloudflare-worker"}},
		{kind: "cloud_run", patterns: []string{"cloud-run"}},
	}
	terraformRuntimeServiceTargetPattern = regexp.MustCompile(`(?i)\b(name|app_name|app_repo)\b\s*=\s*"([^"]+)"`)
)

func discoverTerraformRuntimeServiceModuleEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, block := range terraformModuleBlockPattern.FindAllStringSubmatch(content, -1) {
		if len(block) < 3 {
			continue
		}
		moduleName := strings.TrimSpace(block[1])
		body := block[2]
		platformKind := terraformRuntimeServiceModuleKind(body)
		if platformKind == "" {
			continue
		}
		sourceRef := firstTerraformSourceAssignment(body)
		evidenceKind := EvidenceKind(terraformRuntimeServiceEvidenceKind(platformKind))
		for _, match := range terraformRuntimeServiceTargetPattern.FindAllStringSubmatch(body, -1) {
			if len(match) < 3 {
				continue
			}
			attribute := strings.TrimSpace(strings.ToLower(match[1]))
			candidate := strings.TrimSpace(match[2])
			if candidate == "" {
				continue
			}
			evidence = append(evidence, matchCatalog(
				sourceRepoID,
				candidate,
				filePath,
				evidenceKind,
				RelProvisionsDependencyFor,
				0.96,
				"Terraform runtime service module references the target repository",
				"terraform-runtime-service-module",
				catalog,
				seen,
				map[string]any{
					"module_name":           moduleName,
					"source_ref":            sourceRef,
					"terraform_attribute":   attribute,
					"runtime_platform_kind": platformKind,
				},
			)...)
		}
	}
	return evidence
}

func terraformRuntimeServiceModuleKind(body string) string {
	source := strings.TrimSpace(strings.ToLower(firstTerraformSourceAssignment(body)))
	if source == "" {
		return ""
	}
	for _, family := range terraformRuntimeServiceModuleFamilies {
		for _, pattern := range family.patterns {
			if strings.Contains(source, pattern) {
				return family.kind
			}
		}
	}
	return ""
}

func firstTerraformSourceAssignment(body string) string {
	for _, source := range extractSourceAssignments(body) {
		source = strings.TrimSpace(source)
		if source != "" {
			return source
		}
	}
	return ""
}

func terraformRuntimeServiceEvidenceKind(kind string) string {
	normalized := strings.TrimSpace(strings.ToUpper(kind))
	if normalized == "" {
		normalized = "UNKNOWN"
	}
	return "TERRAFORM_" + normalized + "_SERVICE"
}
