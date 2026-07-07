// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"regexp"
	"strings"
)

type contentMetadata struct {
	ArtifactType    string
	TemplateDialect string
	IACRelevant     bool
}

var (
	goExpressionRE      = regexp.MustCompile(`(?s)\{\{[-~]?.*?[-~]?\}\}`)
	jinjaStatementRE    = regexp.MustCompile(`(?s)\{%-?.*?-?%\}|\{#.*?#\}`)
	githubActionsExprRE = regexp.MustCompile(`(?s)\$\{\{.*?\}\}`)
	goContextRE         = regexp.MustCompile(`\{\{[-~]?\s*(?:\.|\$)`)
	goLineControlRE     = regexp.MustCompile(`(?m)^\s*\{\{[-~]?\s*(if|else|end|with|range|define|template|block)\b`)
	goHintRE            = regexp.MustCompile(`\b(include|toYaml|nindent|tpl)\b`)
	tfInterpolationRE   = regexp.MustCompile(`\$\{`)
	tfDirectiveRE       = regexp.MustCompile(`%\{`)
	tfTemplatefileRE    = regexp.MustCompile(`\btemplatefile\s*\(`)
)

func inferContentMetadata(path string, content string) contentMetadata {
	// These marker scans are the single source of truth for both this
	// function and inferRootFamily below. Each regex here MUST run at most
	// once per inferContentMetadata call: inferRootFamily is only ever
	// called from here (single call site), so it reuses these results
	// through the contentMarkers struct instead of re-invoking MatchString
	// on the same five regexes over the same content.
	//
	// hasAnyGoExpression/hasAnyTFInterpolation/hasAnyTFDirective/
	// hasAnyTFTemplatefile preserve inferRootFamily's original *unfiltered*
	// MatchString semantics exactly (matching `${{ ... }}` GitHub-Actions
	// shapes and escaped `$${`/`%%{` sequences that the filtered variants
	// below intentionally exclude). MatchString short-circuits at the first
	// match, so keeping it separate from the filtered FindAll-based scans
	// below does not reintroduce a second full-content scan.
	hasAnyGoExpression := goExpressionRE.MatchString(content)
	hasAnyJinjaStatement := jinjaStatementRE.MatchString(content)
	hasAnyTFInterpolation := tfInterpolationRE.MatchString(content)
	hasAnyTFDirective := tfDirectiveRE.MatchString(content)
	hasAnyTFTemplatefile := tfTemplatefileRE.MatchString(content)

	goExpressions := filteredMatches(content, goExpressionRE, '$')
	hasCurlyExpressions := len(goExpressions) > 0
	explicitJinja := hasAnyJinjaStatement
	tfInterpolationCount := countUnprefixedMatches(content, tfInterpolationRE, '$')
	tfDirectiveCount := countUnprefixedMatches(content, tfDirectiveRE, '%')
	tfTemplatefileMatches := tfTemplatefileRE.FindAllString(content, -1)
	tfMarkerCount := tfInterpolationCount + tfDirectiveCount + len(tfTemplatefileMatches)
	markers := contentMarkers{
		hasTFMarkers:       hasAnyTFInterpolation || hasAnyTFDirective || hasAnyTFTemplatefile,
		hasGoExpressions:   hasAnyGoExpression,
		hasJinjaStatements: hasAnyJinjaStatement,
	}

	rootFamily := inferRootFamily(path, content, markers)
	artifactType := inferArtifactType(rootFamily, path, content)
	loweredContent := strings.ToLower(content)
	if artifactType == "generic_config" || artifactType == "generic_config_template" {
		switch {
		case strings.Contains(loweredContent, "server {") ||
			strings.Contains(loweredContent, "fastcgi_pass") ||
			strings.Contains(loweredContent, "proxy_pass") ||
			strings.Contains(loweredContent, "location /"):
			if strings.HasSuffix(artifactType, "_template") {
				artifactType = "nginx_config_template"
			} else {
				artifactType = "nginx_config"
			}
		case strings.Contains(loweredContent, "<virtualhost") ||
			strings.Contains(loweredContent, "rewriterule") ||
			strings.Contains(loweredContent, "documentroot") ||
			strings.Contains(loweredContent, "servername "):
			if strings.HasSuffix(artifactType, "_template") {
				artifactType = "apache_config_template"
			} else {
				artifactType = "apache_config"
			}
		}
	}

	suffixes := splitSuffixes(path)
	lastSuffix := ""
	if len(suffixes) > 0 {
		lastSuffix = suffixes[len(suffixes)-1]
	}
	if len(suffixes) >= 2 && isJinjaSuffix(suffixes[len(suffixes)-1]) && isYAMLSuffix(suffixes[len(suffixes)-2]) {
		lastSuffix = suffixes[len(suffixes)-2]
	}

	explicitGo := goContextRE.MatchString(content) || goLineControlRE.MatchString(content)
	for _, expression := range goExpressions {
		if len(goHintRE.FindAllString(expression, -1)) > 0 {
			explicitGo = true
			break
		}
	}
	hasGitHubActions := githubActionsExprRE.MatchString(content)

	templateDialect := ""
	bucket := "plain_text"
	switch {
	case lastSuffix == ".tpl" && rootFamily == "helm_argo":
		bucket = "helm_helper_tpl"
		templateDialect = "go_template"
	case isJinjaSuffix(lastSuffix) && rootFamily == "terraform":
		bucket = "unknown_templated"
		templateDialect = "terraform_template"
	case isJinjaSuffix(lastSuffix):
		bucket = "unknown_templated"
		templateDialect = "jinja"
	case isTerraformTemplateSuffix(lastSuffix) && rootFamily == "terraform":
		bucket = "unknown_templated"
		templateDialect = "terraform_template"
	case isHCLSuffix(lastSuffix):
		bucket = "terraform_hcl"
		if tfMarkerCount > 0 {
			bucket = "terraform_hcl_templated"
			templateDialect = "terraform_template"
		}
	case strings.HasPrefix(artifactType, "ansible_"):
		bucket = artifactType
		if explicitJinja || hasCurlyExpressions {
			templateDialect = "jinja"
		}
	case !isYAMLSuffix(lastSuffix) && lastSuffix != ".kcl":
		if templateDialect == "" && (explicitJinja || hasCurlyExpressions || hasGitHubActions || tfMarkerCount > 0) {
			bucket = "unknown_templated"
		}
	case hasGitHubActions && !explicitGo && !explicitJinja && !hasCurlyExpressions:
		bucket = "unknown_templated"
		templateDialect = "github_actions"
	case !explicitGo && !explicitJinja && !hasCurlyExpressions:
		bucket = "plain_yaml"
	case explicitGo && explicitJinja:
		bucket = "unknown_templated"
	case rootFamily == "ansible_jinja" || rootFamily == "dagster_jinja":
		if explicitGo {
			bucket = "unknown_templated"
		} else {
			bucket = "jinja_yaml"
			templateDialect = "jinja"
		}
	case rootFamily == "helm_argo":
		if explicitJinja {
			bucket = "unknown_templated"
		} else {
			bucket = "go_template_yaml"
			templateDialect = "go_template"
		}
	case explicitJinja && hasCurlyExpressions:
		bucket = "unknown_templated"
	case explicitJinja:
		bucket = "jinja_yaml"
		templateDialect = "jinja"
	case explicitGo:
		bucket = "go_template_yaml"
		templateDialect = "go_template"
	case hasCurlyExpressions:
		bucket = "unknown_templated"
	}

	return contentMetadata{
		ArtifactType:    persistedArtifactType(bucket, artifactType),
		TemplateDialect: templateDialect,
		IACRelevant:     isIACRelevant(rootFamily, path, artifactType, bucket),
	}
}

func filteredMatches(content string, expression *regexp.Regexp, disallowedPrefix byte) []string {
	indexes := expression.FindAllStringIndex(content, -1)
	matches := make([]string, 0, len(indexes))
	for _, index := range indexes {
		start := index[0]
		if start > 0 && content[start-1] == disallowedPrefix {
			continue
		}
		matches = append(matches, content[index[0]:index[1]])
	}
	return matches
}

func countUnprefixedMatches(content string, expression *regexp.Regexp, disallowedPrefix byte) int {
	return len(filteredMatches(content, expression, disallowedPrefix))
}

// contentMarkers carries the template/interpolation marker signals that
// inferContentMetadata computes once from the shared marker regexes
// (goExpressionRE, jinjaStatementRE, tfInterpolationRE, tfDirectiveRE,
// tfTemplatefileRE) and passes down to inferRootFamily. inferRootFamily
// reuses these instead of re-invoking the same regexes over the same
// content: prior to this hoist, each of these five regexes ran twice per
// inferContentMetadata call (once inside inferRootFamily's own MatchString
// probes, once again in inferContentMetadata's own
// filteredMatches/countUnprefixedMatches calls) -- pure duplicated work,
// since inferRootFamily is only ever called from inferContentMetadata
// (single call site).
type contentMarkers struct {
	hasTFMarkers       bool
	hasGoExpressions   bool
	hasJinjaStatements bool
}

func inferRootFamily(path string, content string, markers contentMarkers) string {
	parts := pathParts(path)
	name := strings.ToLower(filepath.Base(path))
	suffixes := splitSuffixes(path)

	switch {
	case anySuffix(suffixes, isHCLSuffix):
		return "terraform"
	case markers.hasTFMarkers &&
		(anySuffix(suffixes, isTerraformTemplateSuffix) || anySuffix(suffixes, isJinjaSuffix)) &&
		!markers.hasGoExpressions &&
		!markers.hasJinjaStatements:
		return "terraform"
	case len(suffixes) > 0 && suffixes[len(suffixes)-1] == ".tpl" &&
		hasPart(parts, "templates") && markers.hasGoExpressions &&
		(name == "_helpers.tpl" || strings.Contains(content, ".Chart") ||
			strings.Contains(content, ".Release") || strings.Contains(content, ".Values") ||
			strings.Contains(content, `{{ include "`) || strings.Contains(content, `{{- include "`) ||
			strings.Contains(content, `{{ define "`) || strings.Contains(content, `{{- define "`)):
		return "helm_argo"
	case name == "chart.yaml" || strings.HasPrefix(name, "values.") ||
		(hasPart(parts, "chart") && hasPart(parts, "templates")) ||
		hasPart(parts, "argocd"):
		return "helm_argo"
	case hasPart(parts, "roles", "playbooks", "handlers", "tasks", "group_vars", "host_vars", "inventory", "inventories"):
		return "ansible_jinja"
	case hasPart(parts, "dagster", "assets", "data_quality", "data_lakehouse"):
		return "dagster_jinja"
	default:
		return "generic"
	}
}

func inferArtifactType(rootFamily string, path string, content string) string {
	name := strings.ToLower(filepath.Base(path))
	parts := pathParts(path)
	suffixes := splitSuffixes(path)
	lastSuffix := ""
	if len(suffixes) > 0 {
		lastSuffix = suffixes[len(suffixes)-1]
	}
	isTemplateSuffix := isJinjaSuffix(lastSuffix) || isTerraformTemplateSuffix(lastSuffix)
	ansibleType := ansibleArtifactType(parts, name, content)

	switch {
	case ansibleType != "":
		return ansibleType
	case hasPart(parts, ".github") && hasPart(parts, "workflows"):
		return "github_actions_workflow"
	case name == "dockerfile" || strings.HasPrefix(name, "dockerfile."):
		return "dockerfile"
	case isDockerComposeFilename(name):
		return "docker_compose"
	case anySuffix(suffixes, isRawConfigSuffix):
		switch {
		case hasPart(parts, "apache", "httpd", "mods-available"):
			if isTemplateSuffix {
				return "apache_config_template"
			}
			return "apache_config"
		case hasPart(parts, "nginx"):
			if isTemplateSuffix {
				return "nginx_config_template"
			}
			return "nginx_config"
		default:
			if isTemplateSuffix {
				return "generic_config_template"
			}
			return "generic_config"
		}
	case anySuffix(suffixes, isYAMLSuffix) && anySuffix(suffixes, isJinjaSuffix):
		return "yaml_template"
	case isJinjaSuffix(lastSuffix):
		if rootFamily == "terraform" {
			return "terraform_template_text"
		}
		return "jinja_text_template"
	case isTerraformTemplateSuffix(lastSuffix):
		if rootFamily == "terraform" {
			return "terraform_template_text"
		}
		return "text_template"
	case anySuffix(suffixes, isHCLSuffix):
		return "terraform_hcl"
	case anySuffix(suffixes, isYAMLSuffix):
		return "yaml_document"
	default:
		return "plain_text"
	}
}

func ansibleArtifactType(parts map[string]struct{}, name string, content string) string {
	switch {
	case hasPart(parts, "inventories", "inventory"):
		return "ansible_inventory"
	case hasPart(parts, "group_vars", "host_vars"):
		return "ansible_vars"
	case hasPart(parts, "playbooks"):
		return "ansible_playbook"
	case hasPart(parts, "roles"):
		if hasPart(parts, "tasks") && (name == "main.yml" || name == "main.yaml") {
			return "ansible_task_entrypoint"
		}
		if hasPart(parts, "vars", "defaults") {
			return "ansible_vars"
		}
		return "ansible_role"
	case isAnsiblePlaybookContent(content):
		return "ansible_playbook"
	default:
		return ""
	}
}

func isAnsiblePlaybookContent(content string) bool {
	lowered := strings.ToLower(content)
	return strings.Contains(lowered, "\nhosts:") ||
		strings.Contains(lowered, "\nroles:") ||
		strings.Contains(lowered, "\nvars_files:") ||
		strings.Contains(lowered, "\nimport_playbook:")
}

func persistedArtifactType(bucket string, artifactType string) string {
	switch bucket {
	case "helm_helper_tpl":
		return "helm_helper_tpl"
	case "go_template_yaml":
		return "go_template_yaml"
	case "jinja_yaml":
		return "jinja_yaml"
	case "terraform_hcl", "terraform_hcl_templated":
		return "terraform_hcl"
	default:
		if artifactType == "plain_text" || artifactType == "yaml_document" {
			return ""
		}
		return artifactType
	}
}

func isIACRelevant(rootFamily string, path string, artifactType string, bucket string) bool {
	if artifactType == "github_actions_workflow" {
		return false
	}
	switch rootFamily {
	case "helm_argo", "ansible_jinja", "terraform", "dagster_jinja":
		return true
	}
	switch artifactType {
	case "apache_config", "apache_config_template", "docker_compose", "dockerfile",
		"nginx_config", "nginx_config_template", "terraform_template_text",
		"yaml_template", "generic_config_template":
		return true
	}
	switch bucket {
	case "go_template_yaml", "jinja_yaml", "terraform_hcl", "terraform_hcl_templated":
		return true
	default:
		return hasPart(pathParts(path), "iac")
	}
}

func pathParts(path string) map[string]struct{} {
	parts := make(map[string]struct{})
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		normalized := strings.ToLower(strings.TrimSpace(part))
		if normalized != "" {
			parts[normalized] = struct{}{}
		}
	}
	return parts
}

func hasPart(parts map[string]struct{}, values ...string) bool {
	for _, value := range values {
		if _, ok := parts[strings.ToLower(value)]; ok {
			return true
		}
	}
	return false
}

func anySuffix(suffixes []string, match func(string) bool) bool {
	for _, suffix := range suffixes {
		if match(suffix) {
			return true
		}
	}
	return false
}

func isYAMLSuffix(suffix string) bool {
	return suffix == ".yaml" || suffix == ".yml"
}

func isHCLSuffix(suffix string) bool {
	return suffix == ".hcl" || suffix == ".tf" || suffix == ".tfvars"
}

func isJinjaSuffix(suffix string) bool {
	return suffix == ".jinja" || suffix == ".jinja2" || suffix == ".j2"
}

func isTerraformTemplateSuffix(suffix string) bool {
	return suffix == ".tpl" || suffix == ".tftpl"
}

func isRawConfigSuffix(suffix string) bool {
	return suffix == ".conf" || suffix == ".cfg" || suffix == ".cnf"
}

func isDockerComposeFilename(name string) bool {
	return name == "compose.yaml" ||
		name == "compose.yml" ||
		name == "docker-compose.yaml" ||
		name == "docker-compose.yml" ||
		(strings.HasPrefix(name, "docker-compose.") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")))
}
