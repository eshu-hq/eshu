package parser

import (
	"regexp"
	"sort"
)

var (
	terragruntReadConfigPattern          = regexp.MustCompile(`(?i)read_terragrunt_config\((?:find_in_parent_folders\()?"([^"]+)"`)
	terragruntFindInParentFoldersPattern = regexp.MustCompile(`(?i)find_in_parent_folders\("([^"]+)"\)`)
	terragruntIncludePathPattern         = regexp.MustCompile(`(?i)\bpath\s*=\s*find_in_parent_folders\("([^"]+)"\)`)
)

type terragruntHelperPaths struct {
	includePaths             []string
	readConfigPaths          []string
	findInParentFoldersPaths []string
	localConfigAssetPaths    []string
}

func parseTerragruntHelperPaths(source []byte, path string) terragruntHelperPaths {
	content := string(source)
	localAssignments := collectTerragruntLocalAssignments(content, path)
	return terragruntHelperPaths{
		includePaths:             collectNormalizedHelperPaths(content, terragruntIncludePathPattern),
		readConfigPaths:          collectNormalizedHelperPaths(content, terragruntReadConfigPattern),
		findInParentFoldersPaths: collectNormalizedHelperPaths(content, terragruntFindInParentFoldersPattern),
		localConfigAssetPaths:    extractTerragruntLocalConfigAssetPaths(content, localAssignments),
	}
}

func collectNormalizedHelperPaths(content string, pattern *regexp.Regexp) []string {
	matches := pattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		normalized := normalizeLocalConfigAssetPath(match)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		paths = append(paths, normalized)
	}
	sort.Strings(paths)
	return paths
}

func normalizeLocalConfigAssetPath(match []string) string {
	if len(match) < 2 {
		return ""
	}
	return normalizeLocalConfigAssetPathValue(match[1], nil)
}
