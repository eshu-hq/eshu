// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"dart",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveDartImportCallee,
		},
	)
}

func resolveDartImportCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	callName := ctx.callName()
	if callName == "" || codeCallHasQualifiedScope(ctx.call, ctx.language) {
		return "", "", ""
	}
	paths := ctx.repositoryImports[callName]
	if len(paths) == 0 {
		return "", "", ""
	}

	var resolvedEntityID string
	for _, importedPath := range dartMatchedImportPaths(ctx, paths) {
		entityID := ctx.index.uniqueNameByPath[importedPath][callName]
		if entityID == "" || entityID == resolvedEntityID {
			continue
		}
		if resolvedEntityID != "" {
			return "", "", ""
		}
		resolvedEntityID = entityID
	}
	if resolvedEntityID == "" {
		return "", "", ""
	}
	return resolvedEntityID, ctx.index.entityFileByID[resolvedEntityID], codeprovenance.MethodImportBinding
}

func dartImportCallBlocksRepoFallback(ctx codeCallResolveContext) bool {
	callName := ctx.callName()
	if callName == "" || codeCallHasQualifiedScope(ctx.call, ctx.language) {
		return false
	}
	if len(dartMatchedImportPaths(ctx, ctx.repositoryImports[callName])) > 0 {
		return true
	}
	for _, entry := range dartDirectiveEntries(ctx.fileData) {
		for _, source := range dartImportEntrySources(entry) {
			if strings.EqualFold(dartImportSourceBaseName(source), callName) {
				return true
			}
		}
	}
	return false
}

func dartMatchedImportPaths(ctx codeCallResolveContext, candidatePaths []string) []string {
	if len(candidatePaths) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var matched []string
	for _, entry := range dartImportEntries(ctx.fileData) {
		for _, source := range dartImportEntrySources(entry) {
			for _, expectedPath := range dartImportSourceCandidates(
				ctx.rawPath,
				ctx.relativePath,
				source,
			) {
				for _, candidatePath := range candidatePaths {
					normalized := normalizeCodeCallPath(candidatePath)
					if normalized == "" || normalized != expectedPath {
						continue
					}
					if _, ok := seen[normalized]; ok {
						continue
					}
					seen[normalized] = struct{}{}
					matched = append(matched, normalized)
				}
			}
		}
	}
	return matched
}

func dartImportEntries(fileData map[string]any) []map[string]any {
	var entries []map[string]any
	for _, entry := range dartDirectiveEntries(fileData) {
		switch strings.TrimSpace(anyToString(entry["import_type"])) {
		case "export", "reexport":
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func dartDirectiveEntries(fileData map[string]any) []map[string]any {
	var entries []map[string]any
	for _, entry := range mapSlice(fileData["imports"]) {
		lang := strings.TrimSpace(anyToString(entry["lang"]))
		if lang != "" && lang != "dart" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func dartImportEntrySources(entry map[string]any) []string {
	sources := codeCallImportEntrySources(entry)
	if source := strings.TrimSpace(anyToString(entry["name"])); source != "" {
		for _, existing := range sources {
			if existing == source {
				return sources
			}
		}
		sources = append(sources, source)
	}
	return sources
}

func dartImportSourceCandidates(rawPath string, relativePath string, source string) []string {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	var candidates []string
	appendCandidate := func(path string) {
		normalized := normalizeCodeCallPath(path)
		if normalized == "" {
			return
		}
		for _, existing := range candidates {
			if existing == normalized {
				return
			}
		}
		candidates = append(candidates, normalized)
	}

	appendCandidate(source)
	callerPath := normalizeCodeCallPath(rawPath)
	if callerPath == "" {
		callerPath = normalizeCodeCallPath(relativePath)
	}
	if callerPath != "" && !strings.Contains(source, ":") && !filepath.IsAbs(source) {
		appendCandidate(filepath.Join(filepath.Dir(callerPath), source))
	}

	repositoryRoot := codeCallRepositoryRoot(rawPath, relativePath)
	if repositoryRoot == "" {
		return candidates
	}
	if strings.HasPrefix(source, "package:") {
		for _, candidate := range dartPackageImportSourceCandidates(rawPath, relativePath, source) {
			appendCandidate(candidate)
		}
		return candidates
	}
	if !filepath.IsAbs(source) {
		appendCandidate(filepath.Join(repositoryRoot, source))
		appendCandidate(filepath.Join(repositoryRoot, "lib", source))
	}
	return candidates
}

func dartPackageImportSourceCandidates(rawPath string, relativePath string, source string) []string {
	packagePath := strings.TrimPrefix(strings.TrimSpace(source), "package:")
	slash := strings.Index(packagePath, "/")
	if slash <= 0 || slash >= len(packagePath)-1 {
		return nil
	}
	packageName := packagePath[:slash]
	packageRelativePath := packagePath[slash+1:]

	var candidates []string
	appendCandidate := func(value string) {
		normalized := normalizeCodeCallPath(value)
		if normalized == "" {
			return
		}
		for _, existing := range candidates {
			if existing == normalized {
				return
			}
		}
		candidates = append(candidates, normalized)
	}
	for _, callerPath := range []string{rawPath, relativePath} {
		packageRoot := dartCallerPackageRoot(callerPath)
		if packageRoot == "" || filepath.Base(packageRoot) != packageName {
			continue
		}
		appendCandidate(filepath.Join(packageRoot, "lib", packageRelativePath))
	}
	return candidates
}

func dartCallerPackageRoot(callerPath string) string {
	normalized := normalizeCodeCallPath(callerPath)
	if normalized == "" {
		return ""
	}
	slashed := filepath.ToSlash(normalized)
	index := strings.LastIndex(slashed, "/lib/")
	if index <= 0 {
		return ""
	}
	return normalizeCodeCallPath(slashed[:index])
}

func dartImportSourceBaseName(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	if strings.HasPrefix(source, "package:") {
		source = strings.TrimPrefix(source, "package:")
		if slash := strings.Index(source, "/"); slash >= 0 && slash < len(source)-1 {
			source = source[slash+1:]
		}
	}
	base := filepath.Base(filepath.ToSlash(source))
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
