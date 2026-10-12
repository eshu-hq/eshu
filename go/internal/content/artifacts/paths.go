// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"path"
	"strings"
)

// CleanRelativePath normalizes a repository-relative path: trimmed, cleaned
// with path.Clean and without a leading "./". The root and an empty path
// become the empty string.
//
// The step is not idempotent when the cleaned result has leading or trailing
// whitespace (the whitespace trim runs before the clean), so a stored,
// already-cleaned path must go through StripRepositoryName only, never
// through this function or normalizeRepoScopedPath again.
func CleanRelativePath(relativePath string) string {
	relativePath = path.Clean(strings.TrimSpace(relativePath))
	switch relativePath {
	case "", ".", "/":
		return ""
	default:
		return strings.TrimPrefix(relativePath, "./")
	}
}

// StripRepositoryName removes the repository-name component, and everything
// before it, from an already-cleaned path (CleanRelativePath). It is the
// read-time name step for name-scoped evidence kinds (NameScoped): a path
// whose first component sequence ends in a component equal to the trimmed
// name loses that prefix, and a path that would become empty or "." keeps the
// name. A blank name, or an empty path, returns the path unchanged.
func StripRepositoryName(cleanedPath, repoName string) string {
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

// normalizeRepoScopedPath is the live path's name step for one raw path: the
// clean step, then the strip step. A stored read model keeps the result of
// the clean step and applies only the strip step at read time.
func normalizeRepoScopedPath(pathValue, repoName string) string {
	return StripRepositoryName(CleanRelativePath(pathValue), repoName)
}

// normalizeTerragruntParentConfigPath returns the config path of a
// find_in_parent_folders match, defaulting to terragrunt.hcl.
func normalizeTerragruntParentConfigPath(match []string) string {
	for _, candidate := range match[1:] {
		cleaned := strings.TrimSpace(candidate)
		if cleaned != "" {
			return normalizeLocalConfigAssetPathValue(cleaned, nil)
		}
	}
	return "terragrunt.hcl"
}
