// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/javascript"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// resolveGenericCallee resolves a parser-emitted call/reference to a callee
// entity by an ordered fallback dispatch. It returns the callee entity id, the
// callee file path, and the closed resolution-provenance method (ADR #2222)
// describing which branch produced the match. The method records how the edge
// was resolved; it never gates resolution. An unresolved call returns empty
// strings and an empty method.
func resolveGenericCallee(
	index shared.EntityIndex,
	repositoryID string,
	repositoryImports map[string][]string,
	reexportIndex shared.ReexportIndex,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	call map[string]any,
) (string, string, codeprovenance.Method) {
	language := shared.CallLanguage(call, rawPath, relativePath)
	ctx := shared.ResolveContext{
		Index:             index,
		RepositoryID:      repositoryID,
		RepositoryImports: repositoryImports,
		ReexportIndex:     reexportIndex,
		RawPath:           rawPath,
		RelativePath:      relativePath,
		FileData:          fileData,
		Call:              call,
		Language:          language,
	}
	if shared.PrefersImportedQualifiedTarget(call, language) {
		if entityID, calleeFile := shared.ResolveImportedCrossFileCallee(
			index,
			repositoryImports,
			reexportIndex,
			repositoryID,
			rawPath,
			relativePath,
			fileData,
			call,
		); entityID != "" {
			return entityID, calleeFile, codeprovenance.MethodImportBinding
		}
	}
	if entityID, calleeFile, method := shared.ResolveSymbolCallee(index, call); entityID != "" {
		return entityID, calleeFile, method
	}

	callLine := shared.PayloadInt(call["line_number"], call["ref_line"])
	if entityID := resolveSameFileScopedCalleeEntityID(index, rawPath, relativePath, call, callLine); entityID != "" {
		return entityID, shared.PreferredPath(rawPath, relativePath), codeprovenance.MethodSameFile
	}
	if entityID := javascript.ResolveDynamicCallee(index, rawPath, relativePath, fileData, call); entityID != "" {
		return entityID, shared.PreferredPath(rawPath, relativePath), codeprovenance.MethodTypeInferred
	}
	if entityID := shared.ResolveSameFileCalleeEntityID(index, rawPath, relativePath, call); entityID != "" {
		return entityID, shared.PreferredPath(rawPath, relativePath), codeprovenance.MethodSameFile
	}
	if shared.PrefersImportedTargetBeforeRepoFallback(call, language) {
		if entityID, calleeFile := shared.ResolveImportedCrossFileCallee(
			index,
			repositoryImports,
			reexportIndex,
			repositoryID,
			rawPath,
			relativePath,
			fileData,
			call,
		); entityID != "" {
			return entityID, calleeFile, codeprovenance.MethodImportBinding
		}
	}

	if entityID, calleeFile, method := resolveLanguageSpecificCallee(
		ctx,
		shared.PhaseBeforeRepoFallback,
	); entityID != "" {
		return entityID, calleeFile, method
	}
	if shared.PrefersImportedTargetBeforeRepoFallback(call, language) &&
		shared.HasRepositoryImportedTargetBinding(
			repositoryImports,
			shared.RepositoryImportPathsForResolution(index, repositoryID, repositoryImports),
			rawPath,
			relativePath,
			fileData,
			call,
		) {
		return "", "", ""
	}
	if language == "python" &&
		shared.PrefersImportedTargetBeforeRepoFallback(call, language) &&
		shared.HasExplicitImportedTarget(fileData, call) {
		return "", "", ""
	}
	if codeCallLanguageResolverBlocksRepoFallback(ctx) {
		return "", "", ""
	}
	for _, name := range shared.ExactCandidateNames(call, language) {
		if entityID := index.UniqueNameByRepo(repositoryID, name); entityID != "" {
			return entityID, index.EntityFileByID(entityID), codeprovenance.MethodRepoUniqueName
		}
	}
	if !shared.HasQualifiedScope(call, language) {
		for _, name := range shared.BroadCandidateNames(call, language) {
			if entityID := index.UniqueNameByRepo(repositoryID, name); entityID != "" {
				return entityID, index.EntityFileByID(entityID), codeprovenance.MethodRepoUniqueName
			}
		}
	}

	if entityID, calleeFile, method := resolveLanguageSpecificCallee(
		ctx,
		shared.PhaseAfterRepoFallback,
	); entityID != "" {
		return entityID, calleeFile, method
	}

	entityID, calleeFile := shared.ResolveImportedCrossFileCallee(
		index,
		repositoryImports,
		reexportIndex,
		repositoryID,
		rawPath,
		relativePath,
		fileData,
		call,
	)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, calleeFile, codeprovenance.MethodImportBinding
}
