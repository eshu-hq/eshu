// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// CollectRepositoryIDs returns the sorted, deduplicated set of repository IDs
// referenced by "repository" or "file" facts in envelopes.
func CollectRepositoryIDs(envelopes []facts.Envelope) []string {
	repositorySet := make(map[string]struct{})
	for _, env := range envelopes {
		switch env.FactKind {
		case "repository", "file":
			repositoryID := payloadcore.PayloadStr(env.Payload, "repo_id")
			if repositoryID == "" {
				repositoryID = payloadcore.PayloadStr(env.Payload, "graph_id")
			}
			if repositoryID != "" {
				repositorySet[repositoryID] = struct{}{}
			}
		}
	}

	repositoryIDs := make([]string, 0, len(repositorySet))
	for repositoryID := range repositorySet {
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	sort.Strings(repositoryIDs)
	return repositoryIDs
}

// CollectRepositoryImports collects the per-repository imports_map fact
// payloads into a normalized symbolName -> file-path-list map.
func CollectRepositoryImports(
	envelopes []facts.Envelope,
) map[string]map[string][]string {
	repositoryImports := make(map[string]map[string][]string)
	for _, env := range envelopes {
		if env.FactKind != "repository" {
			continue
		}
		repositoryID := payloadcore.PayloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = payloadcore.PayloadStr(env.Payload, "graph_id")
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
		path = NormalizePath(path)
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
					appendPath(name, payloadcore.AnyToString(rawPath))
				}
			}
		}
	}

	return result
}

// PrefersImportedQualifiedTarget reports whether a JavaScript-family call
// with a qualified full_name should prefer its import binding before other
// resolution strategies.
func PrefersImportedQualifiedTarget(call map[string]any, language string) bool {
	return JavaScriptFamily(language) && codeCallHasQualifiedFullName(payloadcore.AnyToString(call["full_name"]))
}

// PrefersImportedTargetBeforeRepoFallback reports whether an unqualified
// JavaScript-family or Python call should prefer its import binding before
// the generic repo-unique-name fallback.
func PrefersImportedTargetBeforeRepoFallback(call map[string]any, language string) bool {
	if codeCallHasQualifiedFullName(payloadcore.AnyToString(call["full_name"])) {
		return false
	}
	return JavaScriptFamily(language) || language == "python"
}

// HasRepositoryImportedTargetBinding reports whether the call's import
// metadata binds to a known repository import path, so the dispatch can
// refuse to guess when an explicit (but unresolved) import exists.
func HasRepositoryImportedTargetBinding(
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
	language := CallLanguage(call, rawPath, relativePath)
	for _, target := range codeCallImportedTargets(payloadcore.MapSlice(fileData["imports"]), call) {
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

// CacheRepositoryImportPaths flattens each repository's normalized import
// map once before the per-call resolution loop.
func CacheRepositoryImportPaths(
	index *EntityIndex,
	repositoryImports map[string]map[string][]string,
) {
	if index.repositoryImportPathsByRepo == nil {
		index.repositoryImportPathsByRepo = make(map[string][]string, len(repositoryImports))
	}
	for repositoryID, imports := range repositoryImports {
		index.repositoryImportPathsByRepo[repositoryID] = RepositoryImportPaths(imports)
	}
}

// RepositoryImportPathsForResolution returns the extraction cache when
// available. The fallback preserves direct resolver callers that construct an
// index without running the extraction path first.
func RepositoryImportPathsForResolution(
	index EntityIndex,
	repositoryID string,
	repositoryImports map[string][]string,
) []string {
	if len(repositoryImports) == 0 {
		return nil
	}
	if paths, ok := index.repositoryImportPathsByRepo[repositoryID]; ok {
		return paths
	}
	return RepositoryImportPaths(repositoryImports)
}

// RepositoryImportPaths flattens a symbolName -> file-path-list map into the
// deduplicated, normalized set of file paths it references.
func RepositoryImportPaths(repositoryImports map[string][]string) []string {
	var paths []string
	seen := make(map[string]struct{})
	for _, symbolPaths := range repositoryImports {
		for _, path := range symbolPaths {
			normalized := NormalizePath(path)
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

// ResolveImportedCrossFileCallee resolves a call to a cross-file entity
// through repository import bindings, Python module-path candidates, or one
// bounded re-export hop.
func ResolveImportedCrossFileCallee(
	index EntityIndex,
	repositoryImports map[string][]string,
	reexportIndex ReexportIndex,
	repositoryID string,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	call map[string]any,
) (string, string) {
	importEntries := payloadcore.MapSlice(fileData["imports"])
	for _, target := range codeCallImportedTargets(importEntries, call) {
		language := CallLanguage(call, rawPath, relativePath)
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
				if entityID := index.UniqueNameByPath[matchedPath][target.symbolName]; entityID != "" {
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
