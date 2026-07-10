// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func collectCodeCallRepositoryImports(
	envelopes []facts.Envelope,
) map[string]map[string][]string {
	repositoryImports := make(map[string]map[string][]string)
	for _, env := range envelopes {
		if env.FactKind != "repository" {
			continue
		}
		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = payloadStr(env.Payload, "graph_id")
		}
		if repositoryID == "" {
			continue
		}
		imports, ok := env.Payload["imports_map"]
		if !ok || imports == nil {
			continue
		}
		normalized := codeCallNormalizeRepositoryImports(imports)
		if len(normalized) == 0 {
			continue
		}
		repositoryImports[repositoryID] = normalized
	}
	return repositoryImports
}

func codeCallNormalizeRepositoryImports(value any) map[string][]string {
	result := make(map[string][]string)

	appendPath := func(name string, path string) {
		name = strings.TrimSpace(name)
		path = normalizeCodeCallPath(path)
		if name == "" || path == "" {
			return
		}
		for _, existing := range result[name] {
			if existing == path {
				return
			}
		}
		result[name] = append(result[name], path)
	}

	switch typed := value.(type) {
	case map[string][]string:
		for name, paths := range typed {
			for _, path := range paths {
				appendPath(name, path)
			}
		}
	case map[string]any:
		for name, rawPaths := range typed {
			switch paths := rawPaths.(type) {
			case []string:
				for _, path := range paths {
					appendPath(name, path)
				}
			case []any:
				for _, rawPath := range paths {
					appendPath(name, anyToString(rawPath))
				}
			}
		}
	}

	return result
}

func codeCallPrefersImportedQualifiedTarget(call map[string]any, language string) bool {
	return codeCallJavaScriptFamily(language) && codeCallHasQualifiedFullName(anyToString(call["full_name"]))
}

func codeCallPrefersImportedTargetBeforeRepoFallback(call map[string]any, language string) bool {
	if codeCallHasQualifiedFullName(anyToString(call["full_name"])) {
		return false
	}
	return codeCallJavaScriptFamily(language) || language == "python"
}

func codeCallHasRepositoryImportedTargetBinding(
	repositoryImports map[string][]string,
	repositoryPaths []string,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	call map[string]any,
) bool {
	if len(repositoryImports) == 0 {
		return false
	}
	language := codeCallLanguage(call, rawPath, relativePath)
	for _, target := range codeCallImportedTargets(mapSlice(fileData["imports"]), call) {
		if codeCallMatchImportedPath(
			rawPath,
			relativePath,
			target.importSource,
			language,
			repositoryImports[target.symbolName],
		) != "" {
			return true
		}
		if codeCallMatchImportedPath(
			rawPath,
			relativePath,
			target.importSource,
			language,
			repositoryPaths,
		) != "" {
			return true
		}
	}
	return false
}

// cacheCodeCallRepositoryImportPaths flattens each repository's normalized
// import map once before the per-call resolution loop.
func cacheCodeCallRepositoryImportPaths(
	index *codeEntityIndex,
	repositoryImports map[string]map[string][]string,
) {
	if index.repositoryImportPathsByRepo == nil {
		index.repositoryImportPathsByRepo = make(map[string][]string, len(repositoryImports))
	}
	for repositoryID, imports := range repositoryImports {
		index.repositoryImportPathsByRepo[repositoryID] = codeCallRepositoryImportPaths(imports)
	}
}

// codeCallRepositoryImportPathsForResolution returns the extraction cache when
// available. The fallback preserves direct resolver callers that construct an
// index without running extractCodeCallRowsWithIndex first.
func codeCallRepositoryImportPathsForResolution(
	index codeEntityIndex,
	repositoryID string,
	repositoryImports map[string][]string,
) []string {
	if len(repositoryImports) == 0 {
		return nil
	}
	if paths, ok := index.repositoryImportPathsByRepo[repositoryID]; ok {
		return paths
	}
	return codeCallRepositoryImportPaths(repositoryImports)
}

func codeCallRepositoryImportPaths(repositoryImports map[string][]string) []string {
	var paths []string
	seen := make(map[string]struct{})
	for _, symbolPaths := range repositoryImports {
		for _, path := range symbolPaths {
			normalized := normalizeCodeCallPath(path)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			paths = append(paths, normalized)
		}
	}
	return paths
}

func resolveGoSameDirectoryCalleeEntityID(
	index codeEntityIndex,
	repositoryID string,
	rawPath string,
	relativePath string,
	call map[string]any,
	language string,
) string {
	dir := codeCallDirectoryKey(codeCallPreferredPath(rawPath, relativePath))
	if repositoryID == "" || dir == "" {
		return ""
	}
	for _, name := range codeCallExactCandidateNames(call, language) {
		if entityID := index.uniqueNameByRepoDir[repositoryID][dir][name]; entityID != "" {
			return entityID
		}
	}
	for _, name := range codeCallBroadCandidateNames(call, language) {
		if entityID := index.uniqueNameByRepoDir[repositoryID][dir][name]; entityID != "" {
			return entityID
		}
	}
	return ""
}

func resolveImportedCrossFileCallee(
	index codeEntityIndex,
	repositoryImports map[string][]string,
	reexportIndex codeCallReexportIndex,
	repositoryID string,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	call map[string]any,
) (string, string) {
	importEntries := mapSlice(fileData["imports"])
	for _, target := range codeCallImportedTargets(importEntries, call) {
		language := codeCallLanguage(call, rawPath, relativePath)
		if len(repositoryImports) > 0 {
			paths := repositoryImports[target.symbolName]
			matchedPath := codeCallMatchImportedPath(
				rawPath,
				relativePath,
				target.importSource,
				language,
				paths,
			)
			if matchedPath != "" {
				if entityID := index.uniqueNameByPath[matchedPath][target.symbolName]; entityID != "" {
					return entityID, index.entityFileByID[entityID]
				}
			}
			if entityID, calleeFile := resolvePythonImportedRepositorySymbolTarget(
				index,
				language,
				rawPath,
				relativePath,
				target.importSource,
				paths,
				target.symbolName,
			); entityID != "" {
				return entityID, calleeFile
			}
		}
		if entityID, calleeFile := resolvePythonImportedSourceCandidateTarget(
			index,
			language,
			rawPath,
			relativePath,
			target,
		); entityID != "" {
			return entityID, calleeFile
		}
		if entityID, calleeFile := resolveReexportedCrossFileCallee(
			index,
			reexportIndex,
			repositoryID,
			rawPath,
			relativePath,
			language,
			target,
		); entityID != "" {
			return entityID, calleeFile
		}
	}

	return "", ""
}
