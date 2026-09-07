// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func BuildRepositoryConfigArtifacts(repoName string, files []querycontract.FileContent) map[string]any {
	configPaths := make([]map[string]any, 0)
	configPaths = append(configPaths, extractKustomizeConfigPathRows(repoName, files)...)
	configPaths = append(configPaths, extractDockerComposeConfigPathRows(repoName, files)...)
	configPaths = append(configPaths, extractHCLConfigAssetRows(repoName, files)...)
	configPaths = append(configPaths, extractAnsibleConfigPathRows(repoName, files)...)
	if len(configPaths) == 0 {
		return nil
	}
	sort.Slice(configPaths, func(i, j int) bool {
		leftPath := querycontract.StringVal(configPaths[i], "path")
		rightPath := querycontract.StringVal(configPaths[j], "path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		leftKind := querycontract.StringVal(configPaths[i], "evidence_kind")
		rightKind := querycontract.StringVal(configPaths[j], "evidence_kind")
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		return querycontract.StringVal(configPaths[i], "relative_path") < querycontract.StringVal(configPaths[j], "relative_path")
	})
	return map[string]any{"config_paths": configPaths}
}

func isConfigArtifactCandidate(file querycontract.FileContent) bool {
	relativePath := strings.TrimSpace(file.RelativePath)
	if relativePath == "" {
		return false
	}

	lowerBase := strings.ToLower(path.Base(relativePath))
	switch {
	case lowerBase == "kustomization.yaml", lowerBase == "kustomization.yml", lowerBase == "kustomization":
		return true
	case strings.HasPrefix(lowerBase, "docker-compose"):
		return true
	case lowerBase == "terragrunt.hcl":
		return true
	case strings.HasSuffix(lowerBase, ".tfvars"), strings.HasSuffix(lowerBase, ".tfvars.json"):
		return true
	case strings.HasSuffix(lowerBase, ".tf"), strings.HasSuffix(lowerBase, ".hcl"):
		return true
	case strings.HasSuffix(lowerBase, ".yaml"), strings.HasSuffix(lowerBase, ".yml"), strings.HasSuffix(lowerBase, ".json"):
		return true
	default:
		return false
	}
}

func appendConfigArtifactRow(rows []map[string]any, seen map[string]struct{}, pathValue, repoName, relativePath, evidenceKind string) []map[string]any {
	pathValue = normalizeRepoScopedPath(pathValue, repoName)
	if pathValue == "" {
		return rows
	}
	key := strings.Join([]string{pathValue, repoName, relativePath, evidenceKind}, "|")
	if _, ok := seen[key]; ok {
		return rows
	}
	seen[key] = struct{}{}
	return append(rows, map[string]any{
		"path":          pathValue,
		"source_repo":   repoName,
		"relative_path": relativePath,
		"evidence_kind": evidenceKind,
	})
}

func normalizeTerragruntParentConfigPath(match []string) string {
	for _, candidate := range match[1:] {
		cleaned := strings.TrimSpace(candidate)
		if cleaned != "" {
			return normalizeLocalConfigAssetPathValue(cleaned, nil)
		}
	}
	return "terragrunt.hcl"
}

func normalizeRepoScopedPath(pathValue, repoName string) string {
	cleanedPath := CleanRepositoryRelativePath(pathValue)
	trimmedRepoName := strings.TrimSpace(repoName)
	if cleanedPath == "" || trimmedRepoName == "" {
		return cleanedPath
	}

	parts := strings.Split(cleanedPath, "/")
	for index := range parts {
		prefix := path.Join(parts[:index+1]...)
		if path.Base(prefix) != trimmedRepoName {
			continue
		}
		suffix := path.Join(parts[index+1:]...)
		if suffix != "" && suffix != "." {
			return suffix
		}
	}
	return cleanedPath
}

func CleanRepositoryRelativePath(relativePath string) string {
	relativePath = path.Clean(strings.TrimSpace(relativePath))
	switch relativePath {
	case "", ".", "/":
		return ""
	default:
		return strings.TrimPrefix(relativePath, "./")
	}
}

// buildRepositoryConfigArtifacts keeps the in-package spelling after the
// #6060 export; repository story tests name BuildRepositoryConfigArtifacts.
func buildRepositoryConfigArtifacts(repoName string, files []querycontract.FileContent) map[string]any {
	return BuildRepositoryConfigArtifacts(repoName, files)
}
