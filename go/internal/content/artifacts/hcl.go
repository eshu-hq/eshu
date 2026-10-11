// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"path"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content/artifacts/prefilter"
)

var (
	terragruntDependencyConfigPathPattern = regexp.MustCompile(`(?im)(?:^|[\r\n])\s*config_path\s*=\s*(.+?)\s*(?:$|[\r\n])`)
	terragruntReadConfigPattern           = regexp.MustCompile(`(?i)read_terragrunt_config\(\s*(?:find_in_parent_folders\(\s*(?:"([^"]+)")?\s*\)|"([^"]+)")`)
	terragruntFindInParentFoldersPattern  = regexp.MustCompile(`(?i)find_in_parent_folders\(\s*(?:"([^"]+)")?\s*\)`)
	terragruntIncludePathPattern          = regexp.MustCompile(`(?i)\bpath\s*=\s*find_in_parent_folders\(\s*(?:"([^"]+)")?\s*\)`)
	localConfigFunctionStartPattern       = regexp.MustCompile(`(?i)\b(?:file|templatefile)\(`)
	localTerraformModuleSourcePattern     = regexp.MustCompile(`(?im)(?:module\s+"[^"]+"\s*\{[^}]*?|\bterraform\s*\{[^}]*?)\bsource\b\s*=\s*(.+?)\s*(?:$|[\r\n])`)
	localStringAssignmentPattern          = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+)\s*=\s*"([^"]+)"\s*$`)
	localAssignmentStartPattern           = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*=\s*(.+?)\s*$`)
	pathRelativeToIncludeSplitPattern     = regexp.MustCompile(`(?is)^split\(\s*"/"\s*,\s*path_relative_to_include\(\s*(?:"[^"]+")?\s*\)\s*\)$`)
	quotedStringPattern                   = regexp.MustCompile(`"([^"]+)"`)
	localInterpolationPattern             = regexp.MustCompile(`\$\{local\.([A-Za-z0-9_]+)\}`)
	joinPathModulePattern                 = regexp.MustCompile(`(?is)^join\(\s*""\s*,\s*\[\s*path\.module\s*,\s*"([^"]+)"\s*\]\s*\)$`)
)

// hclConfigFileKind says how the HCL extractor treats a file by name.
type hclConfigFileKind int

const (
	// hclConfigNone is a file the HCL extractor does not read or emit.
	hclConfigNone hclConfigFileKind = iota
	// hclConfigVarFile (.tfvars, .tfvars.json) emits a row from its path alone;
	// its content is not read.
	hclConfigVarFile
	// hclConfigSourceFile (terragrunt.hcl, .tf, .hcl) is scanned for config
	// asset references, so its content is read.
	hclConfigSourceFile
)

// classifyHCLConfigFile is the selector the HCL extractor branches on. The
// needed-content planner calls it too, so a file the extractor reads is
// always a file the load fetches.
func classifyHCLConfigFile(relativePath string) hclConfigFileKind {
	lowerBase := strings.ToLower(path.Base(relativePath))
	switch {
	case strings.HasSuffix(lowerBase, ".tfvars"), strings.HasSuffix(lowerBase, ".tfvars.json"):
		return hclConfigVarFile
	case lowerBase == "terragrunt.hcl", strings.HasSuffix(lowerBase, ".tf"), strings.HasSuffix(lowerBase, ".hcl"):
		return hclConfigSourceFile
	default:
		return hclConfigNone
	}
}

func extractHCLConfigAssetRows(repoName string, files []File) []map[string]any {
	return extractHCLConfigAssetRowsWith(hclScanPrefilters, repoName, files)
}

// extractHCLConfigAssetRowsWith runs the HCL scans, skipping each one whose
// prefilter proves the file cannot match. The prefilters are a parameter so a
// test can substitute deliberately wrong ones and watch the equality proof
// fail; production always passes hclScanPrefilters.
func extractHCLConfigAssetRowsWith(prefilters *prefilter.Set, repoName string, files []File) []map[string]any {
	rows := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, file := range files {
		switch classifyHCLConfigFile(file.RelativePath) {
		case hclConfigVarFile:
			rows = appendConfigArtifactRow(rows, seen, CleanRelativePath(file.RelativePath), repoName, file.RelativePath, "terraform_var_file")
			continue
		case hclConfigSourceFile:
		default:
			continue
		}
		plan := planHCLScans(prefilters, file.Content)
		var localConfigAssignments map[string]string
		if plan.needsLocalAssignments() {
			localConfigAssignments = extractLocalConfigAssignments(file.RelativePath, file.Content)
		}
		if plan.dependencyConfigPath {
			for _, match := range terragruntDependencyConfigPathPattern.FindAllStringSubmatch(file.Content, -1) {
				if len(match) < 2 {
					continue
				}
				configPath := NormalizeConfigArtifactExpression(match[1], localConfigAssignments)
				if configPath == "" {
					continue
				}
				rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terragrunt_dependency_config_path")
			}
		}
		if plan.readConfig {
			for _, match := range terragruntReadConfigPattern.FindAllStringSubmatch(file.Content, -1) {
				configPath := normalizeTerragruntParentConfigPath(match)
				if configPath == "" {
					continue
				}
				rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terragrunt_read_config")
			}
		}
		if plan.includePath {
			for _, match := range terragruntIncludePathPattern.FindAllStringSubmatch(file.Content, -1) {
				configPath := normalizeTerragruntParentConfigPath(match)
				if configPath == "" {
					continue
				}
				rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terragrunt_include_path")
			}
		}
		if plan.findInParentFolders {
			for _, match := range terragruntFindInParentFoldersPattern.FindAllStringSubmatch(file.Content, -1) {
				configPath := normalizeTerragruntParentConfigPath(match)
				if configPath == "" {
					continue
				}
				rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terragrunt_find_in_parent_folders")
			}
		}
		if plan.configFunction {
			for _, configPath := range extractConfigAssetFunctionPaths(file.Content, localConfigAssignments) {
				if configPath == "" {
					continue
				}
				rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "local_config_asset")
			}
		}
		if plan.moduleSource {
			for _, match := range localTerraformModuleSourcePattern.FindAllStringSubmatch(file.Content, -1) {
				if len(match) < 2 {
					continue
				}
				configPath := NormalizeConfigArtifactExpression(match[1], localConfigAssignments)
				if configPath == "" {
					continue
				}
				rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terraform_module_source_path")
			}
		}
	}
	return rows
}

// NormalizeConfigArtifactExpression resolves one Terraform or Terragrunt
// expression to the local config path it names, or the empty string when it
// names none. It strips inline comments and a trailing comma, reads the path
// out of a file(), templatefile(), lookup() or path-join form, and substitutes
// local variable references from localAssignments (nil is allowed). It keeps
// the value only when the expression is helper-built (path.module, local.,
// join(), get_terragrunt_dir() and the like) or the value looks like a local
// path: a ./, ../ or / prefix, a config file extension, or a slash with no URL
// scheme and no query. The extractors and the query impact handlers share it,
// so both read an expression the same way.
func NormalizeConfigArtifactExpression(expression string, localAssignments map[string]string) string {
	trimmed := strings.TrimSpace(stripHCLInlineComments(expression))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, ",")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return ""
	}

	if value := extractConfigAssetPathFromExpression(trimmed, localAssignments); value != "" {
		if isHelperBuiltConfigExpression(trimmed) || isLocalishConfigArtifactPath(value) {
			return value
		}
	}
	if value := normalizeLocalConfigAssetPathValue(trimmed, localAssignments); value != "" {
		if isHelperBuiltConfigExpression(trimmed) || isLocalishConfigArtifactPath(value) {
			return value
		}
	}
	return ""
}

func isHelperBuiltConfigExpression(expression string) bool {
	lower := strings.ToLower(expression)
	return strings.Contains(lower, "get_repo_root(") ||
		strings.Contains(lower, "path.module") ||
		strings.Contains(lower, "get_parent_terragrunt_dir(") ||
		strings.Contains(lower, "get_terragrunt_dir(") ||
		strings.Contains(lower, "path_relative_to_include(") ||
		strings.Contains(lower, "local.") ||
		strings.Contains(lower, "join(") ||
		strings.Contains(lower, "lookup(") ||
		strings.Contains(lower, "file(") ||
		strings.Contains(lower, "templatefile(")
}

func isLocalishConfigArtifactPath(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, "/") {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.HasSuffix(lower, ".hcl") ||
		strings.HasSuffix(lower, ".tf") ||
		strings.HasSuffix(lower, ".yaml") ||
		strings.HasSuffix(lower, ".yml") ||
		strings.HasSuffix(lower, ".json") ||
		strings.HasSuffix(lower, ".tpl") ||
		strings.HasSuffix(lower, ".tmpl") {
		return true
	}
	return strings.Contains(trimmed, "/") &&
		!strings.Contains(trimmed, "://") &&
		!strings.Contains(trimmed, "?")
}

func extractConfigAssetFunctionPaths(content string, localAssignments map[string]string) []string {
	matches := localConfigFunctionStartPattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	paths := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		openParenIndex := match[1] - 1
		arguments, ok := extractDelimitedContent(content, openParenIndex, '(', ')')
		if !ok {
			continue
		}
		firstArgument := firstTopLevelArgument(arguments)
		if firstArgument == "" {
			continue
		}
		configPath := extractConfigAssetPathFromExpression(firstArgument, localAssignments)
		if configPath == "" {
			continue
		}
		if _, ok := seen[configPath]; ok {
			continue
		}
		seen[configPath] = struct{}{}
		paths = append(paths, configPath)
	}
	return paths
}

func extractConfigAssetPathFromExpression(expression string, localAssignments map[string]string) string {
	trimmed := strings.TrimSpace(stripHCLInlineComments(expression))
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "find_in_parent_folders(") || strings.Contains(trimmed, "read_terragrunt_config(") {
		return ""
	}
	if quoted := exactQuotedString(trimmed); quoted != "" {
		if strings.HasPrefix(quoted, "${") && strings.HasSuffix(quoted, "}") {
			inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(quoted, "${"), "}"))
			if value := extractConfigAssetPathFromExpression(inner, localAssignments); value != "" {
				return value
			}
		}
		if value := normalizeConfigAssetLiteral(quoted, localAssignments); value != "" {
			return value
		}
	}
	if value := extractJoinedConfigAssetPath(trimmed, localAssignments); value != "" {
		return value
	}
	if strings.HasPrefix(trimmed, "local.") {
		localName := strings.TrimSpace(strings.TrimPrefix(trimmed, "local."))
		if localName == "" {
			return ""
		}
		return normalizeLocalConfigAssetPathValue(localAssignments[localName], localAssignments)
	}
	if match := joinPathModulePattern.FindStringSubmatch(trimmed); len(match) >= 2 {
		return normalizeConfigAssetLiteral(match[1], localAssignments)
	}
	for _, match := range quotedStringPattern.FindAllStringSubmatch(expression, -1) {
		if len(match) < 2 {
			continue
		}
		if value := normalizeConfigAssetLiteral(match[1], localAssignments); value != "" {
			return value
		}
	}
	return ""
}

func extractJoinedConfigAssetPath(expression string, localAssignments map[string]string) string {
	trimmed := strings.TrimSpace(expression)
	if !strings.HasPrefix(strings.ToLower(trimmed), "join(") {
		return ""
	}

	openParenIndex := strings.Index(trimmed, "(")
	arguments, ok := extractDelimitedContent(trimmed, openParenIndex, '(', ')')
	if !ok {
		return ""
	}

	parts := splitTopLevelCommaSeparated(arguments)
	if len(parts) != 2 {
		return ""
	}

	arrayExpression := strings.TrimSpace(parts[1])
	if arrayExpression == "" || !strings.HasPrefix(arrayExpression, "[") {
		return ""
	}
	openBracketIndex := strings.Index(arrayExpression, "[")
	listContent, ok := extractDelimitedContent(arrayExpression, openBracketIndex, '[', ']')
	if !ok {
		return ""
	}

	segments := make([]string, 0)
	for _, item := range splitTopLevelCommaSeparated(listContent) {
		resolved := extractConfigAssetPathFromExpression(item, localAssignments)
		if resolved == "" {
			continue
		}
		segments = append(segments, resolved)
	}
	if len(segments) == 0 {
		return ""
	}

	separator := strings.TrimSpace(stripHCLInlineComments(parts[0]))
	separator = strings.Trim(separator, "\"")
	return normalizeLocalConfigAssetPathValue(strings.Join(segments, separator), localAssignments)
}

func normalizeConfigAssetLiteral(value string, localAssignments map[string]string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "/") &&
		!strings.HasSuffix(strings.ToLower(trimmed), ".yaml") &&
		!strings.HasSuffix(strings.ToLower(trimmed), ".yml") &&
		!strings.HasSuffix(strings.ToLower(trimmed), ".json") &&
		!strings.HasSuffix(strings.ToLower(trimmed), ".tpl") &&
		!strings.HasSuffix(strings.ToLower(trimmed), ".tmpl") &&
		!strings.HasSuffix(strings.ToLower(trimmed), ".hcl") &&
		!strings.HasSuffix(strings.ToLower(trimmed), ".tf") {
		return ""
	}
	return normalizeLocalConfigAssetPathValue(trimmed, localAssignments)
}

func exactQuotedString(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || trimmed[0] != '"' || trimmed[len(trimmed)-1] != '"' {
		return ""
	}
	return trimmed[1 : len(trimmed)-1]
}

func normalizeLocalConfigAssetPathValue(value string, localAssignments map[string]string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") || strings.HasPrefix(strings.ToLower(value), "git::") || strings.HasPrefix(strings.ToLower(value), "tfr:///") {
		return ""
	}
	value = resolveLocalInterpolations(value, localAssignments)
	replacer := strings.NewReplacer(
		"${path.module}/", "",
		"${path_relative_to_include()}/", "",
		"${path_relative_to_include()}", "",
		"${get_repo_root()}/", "",
		"${get_parent_terragrunt_dir()}/", "",
		"${get_terragrunt_dir()}/", "",
	)
	value = replacer.Replace(value)
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	value = strings.TrimSpace(value)
	if value == "" || value == "." {
		return ""
	}
	return CleanRelativePath(value)
}

func resolveLocalInterpolations(value string, localAssignments map[string]string) string {
	if len(localAssignments) == 0 {
		return value
	}
	resolved := value
	for range 5 {
		changed := false
		resolved = localInterpolationPattern.ReplaceAllStringFunc(resolved, func(match string) string {
			submatch := localInterpolationPattern.FindStringSubmatch(match)
			if len(submatch) < 2 {
				return match
			}
			replacement := strings.TrimSpace(localAssignments[submatch[1]])
			if replacement == "" {
				return match
			}
			changed = true
			return replacement
		})
		if !changed {
			break
		}
	}
	return resolved
}
