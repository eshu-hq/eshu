// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

type codeCallLanguageResolverPhase string

const (
	codeCallLanguageResolverPhaseBeforeRepoFallback codeCallLanguageResolverPhase = "before_repo_fallback"
	codeCallLanguageResolverPhaseAfterRepoFallback  codeCallLanguageResolverPhase = "after_repo_fallback"
)

type codeCallLanguageResolver struct {
	phase   codeCallLanguageResolverPhase
	resolve func(codeCallResolveContext) (string, string, codeprovenance.Method)
}

type codeCallResolveContext struct {
	index             codeEntityIndex
	repositoryID      string
	repositoryImports map[string][]string
	reexportIndex     codeCallReexportIndex
	rawPath           string
	relativePath      string
	fileData          map[string]any
	call              map[string]any
	language          string
}

func (c codeCallResolveContext) callName() string {
	return strings.TrimSpace(anyToString(c.call["name"]))
}

var codeCallLanguageResolvers = map[string][]codeCallLanguageResolver{}

func registerCodeCallLanguageResolvers(language string, resolvers ...codeCallLanguageResolver) {
	language = strings.TrimSpace(language)
	if language == "" {
		return
	}
	codeCallLanguageResolvers[language] = append(codeCallLanguageResolvers[language], resolvers...)
}

func resolveLanguageSpecificCallee(
	ctx codeCallResolveContext,
	phase codeCallLanguageResolverPhase,
) (string, string, codeprovenance.Method) {
	for _, resolver := range codeCallLanguageResolvers[ctx.language] {
		if resolver.phase != phase || resolver.resolve == nil {
			continue
		}
		entityID, calleeFile, method := resolver.resolve(ctx)
		if entityID != "" {
			return entityID, calleeFile, method
		}
	}
	return "", "", ""
}

func codeCallLanguageResolverBlocksRepoFallback(ctx codeCallResolveContext) bool {
	switch ctx.language {
	case "dart":
		return dartImportCallBlocksRepoFallback(ctx)
	case "elixir":
		return elixirAliasCallBlocksRepoFallback(ctx)
	case "haskell":
		return haskellQualifiedImportTargetExists(ctx)
	case "java":
		return javaImportedReceiverBindingBlocksRepoFallback(ctx)
	case "kotlin":
		return kotlinImportedReceiverBindingBlocksRepoFallback(ctx)
	default:
		return false
	}
}

func resolveGoPackageQualifiedCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	entityID := resolveGoPackageQualifiedCalleeEntityID(ctx.index, ctx.repositoryID, ctx.fileData, ctx.call)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodImportBinding
}

func resolveGoMethodReturnChainCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	entityID := resolveGoMethodReturnChainCalleeEntityID(ctx.index, ctx.repositoryID, ctx.call)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
}

func resolveGoSameDirectoryCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	if codeCallHasQualifiedScope(ctx.call, ctx.language) {
		return "", "", ""
	}
	entityID := resolveGoSameDirectoryCalleeEntityID(
		ctx.index,
		ctx.repositoryID,
		ctx.rawPath,
		ctx.relativePath,
		ctx.call,
		ctx.language,
	)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodScopeUniqueName
}

func resolveGoCrossRepoExportCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	entityID := resolveGoCrossRepoExportCalleeEntityID(ctx.index, ctx.repositoryID, ctx.fileData, ctx.call)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodCrossRepoExportPackage
}
