// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers returns the Go language resolver list in its declared phase order:
// package-qualified import binding, method-return-chain inference, and
// same-directory resolution run before the generic repo-unique-name
// fallback; cross-repo package-export resolution runs after it.
// Each call returns a fresh slice, so no caller can mutate another's view.
func Resolvers() []shared.Resolver {
	return []shared.Resolver{
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveGoPackageQualifiedCallee,
		},
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveGoMethodReturnChainCallee,
		},
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveGoSameDirectoryCallee,
		},
		{
			Phase:   shared.PhaseAfterRepoFallback,
			Resolve: resolveGoCrossRepoExportCallee,
		},
	}
}

func resolveGoPackageQualifiedCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	entityID := resolveGoPackageQualifiedCalleeEntityID(ctx.Index, ctx.RepositoryID, ctx.FileData, ctx.Call)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodImportBinding
}

func resolveGoMethodReturnChainCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	entityID := resolveGoMethodReturnChainCalleeEntityID(ctx.Index, ctx.RepositoryID, ctx.Call)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodTypeInferred
}

func resolveGoSameDirectoryCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	if shared.HasQualifiedScope(ctx.Call, ctx.Language) {
		return "", "", ""
	}
	entityID := resolveGoSameDirectoryCalleeEntityID(
		ctx.Index,
		ctx.RepositoryID,
		ctx.RawPath,
		ctx.RelativePath,
		ctx.Call,
		ctx.Language,
	)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodScopeUniqueName
}

func resolveGoCrossRepoExportCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	entityID := resolveGoCrossRepoExportCalleeEntityID(ctx.Index, ctx.RepositoryID, ctx.FileData, ctx.Call)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodCrossRepoExportPackage
}
