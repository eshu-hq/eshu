// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers is the Dart language resolver list.
var Resolvers = []shared.Resolver{
	{
		Phase:   shared.PhaseBeforeRepoFallback,
		Resolve: resolveDartImportCallee,
	},
}

func resolveDartImportCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	callName := ctx.CallName()
	if callName == "" || shared.HasQualifiedScope(ctx.Call, ctx.Language) {
		return "", "", ""
	}
	paths := ctx.RepositoryImports[callName]
	if len(paths) == 0 {
		return "", "", ""
	}

	var resolvedEntityID string
	for _, importedPath := range dartMatchedImportPaths(ctx, paths) {
		entityID := ctx.Index.UniqueNameByPath[importedPath][callName]
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
	return resolvedEntityID, ctx.Index.EntityFileByID(resolvedEntityID), codeprovenance.MethodImportBinding
}

// BlocksRepoFallback reports whether the Dart file's imports explain (but
// fail to resolve) an unqualified call, so the dispatch must not fall back
// to an ambiguous repo-unique-name guess after the resolver declines.
func BlocksRepoFallback(ctx shared.ResolveContext) bool {
	callName := ctx.CallName()
	if callName == "" || shared.HasQualifiedScope(ctx.Call, ctx.Language) {
		return false
	}
	if len(dartMatchedImportPaths(ctx, ctx.RepositoryImports[callName])) > 0 {
		return true
	}
	for _, entry := range dartDirectiveEntries(ctx.FileData) {
		for _, source := range dartImportEntrySources(entry) {
			if strings.EqualFold(dartImportSourceBaseName(source), callName) {
				return true
			}
		}
	}
	return false
}

func dartMatchedImportPaths(ctx shared.ResolveContext, candidatePaths []string) []string {
	if len(candidatePaths) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var matched []string
	for _, entry := range dartImportEntries(ctx.FileData) {
		for _, source := range dartImportEntrySources(entry) {
			for _, expectedPath := range dartImportSourceCandidates(
				ctx.RawPath,
				ctx.RelativePath,
				source,
			) {
				for _, candidatePath := range candidatePaths {
					normalized := shared.NormalizePath(candidatePath)
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
		switch strings.TrimSpace(payloadcore.AnyToString(entry["import_type"])) {
		case "export", "reexport":
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func dartDirectiveEntries(fileData map[string]any) []map[string]any {
	var entries []map[string]any
	for _, entry := range payloadcore.MapSlice(fileData["imports"]) {
		lang := strings.TrimSpace(payloadcore.AnyToString(entry["lang"]))
		if lang != "" && lang != "dart" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func dartImportEntrySources(entry map[string]any) []string {
	sources := shared.ImportEntrySources(entry)
	if source := strings.TrimSpace(payloadcore.AnyToString(entry["name"])); source != "" {
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
		normalized := shared.NormalizePath(path)
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
	callerPath := shared.NormalizePath(rawPath)
	if callerPath == "" {
		callerPath = shared.NormalizePath(relativePath)
	}
	if callerPath != "" && !strings.Contains(source, ":") && !filepath.IsAbs(source) {
		appendCandidate(filepath.Join(filepath.Dir(callerPath), source))
	}

	repositoryRoot := shared.RepositoryRoot(rawPath, relativePath)
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
		normalized := shared.NormalizePath(value)
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
	normalized := shared.NormalizePath(callerPath)
	if normalized == "" {
		return ""
	}
	slashed := filepath.ToSlash(normalized)
	index := strings.LastIndex(slashed, "/lib/")
	if index <= 0 {
		return ""
	}
	return shared.NormalizePath(slashed[:index])
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
