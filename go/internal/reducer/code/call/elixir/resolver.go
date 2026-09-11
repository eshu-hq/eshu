// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elixir

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers is the Elixir language resolver list.
var Resolvers = []shared.Resolver{
	{
		Phase:   shared.PhaseBeforeRepoFallback,
		Resolve: resolveElixirAliasImportCallee,
	},
}

func resolveElixirAliasImportCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	receiver, methodName, ok := elixirQualifiedCall(ctx.Call)
	if !ok || ctx.RepositoryID == "" {
		return "", "", ""
	}
	moduleName := elixirAliasModuleName(ctx.FileData, receiver)
	if moduleName == "" || len(ctx.RepositoryImports[moduleName]) == 0 {
		return "", "", ""
	}
	entityID := ctx.Index.UniqueNameByRepo[ctx.RepositoryID][moduleName+"."+methodName]
	if entityID == "" {
		return "", "", ""
	}
	calleeFile := ctx.Index.EntityFileByID(entityID)
	if !elixirImportedModuleOwnsFile(ctx, moduleName, calleeFile) {
		return "", "", ""
	}
	return entityID, calleeFile, codeprovenance.MethodImportBinding
}

func elixirQualifiedCall(call map[string]any) (string, string, bool) {
	fullName := strings.TrimSpace(payloadcore.AnyToString(call["full_name"]))
	dot := strings.LastIndex(fullName, ".")
	if dot <= 0 || dot >= len(fullName)-1 {
		return "", "", false
	}
	receiver := strings.TrimSpace(fullName[:dot])
	methodName := strings.TrimSpace(fullName[dot+1:])
	return receiver, methodName, receiver != "" && methodName != ""
}

func elixirAliasModuleName(fileData map[string]any, receiver string) string {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" {
		return ""
	}
	for _, entry := range payloadcore.MapSlice(fileData["imports"]) {
		if strings.TrimSpace(payloadcore.AnyToString(entry["import_type"])) != "alias" {
			continue
		}
		moduleName := strings.TrimSpace(payloadcore.AnyToString(entry["name"]))
		if moduleName == "" {
			continue
		}
		alias := strings.TrimSpace(payloadcore.AnyToString(entry["alias"]))
		if alias == "" {
			alias = shared.TrailingName(moduleName)
		}
		if alias == receiver {
			return moduleName
		}
		if strings.HasPrefix(receiver, alias+".") {
			return moduleName + strings.TrimPrefix(receiver, alias)
		}
	}
	return ""
}

// BlocksRepoFallback reports whether the Elixir file's aliases explain an
// unqualified call, so the dispatch must not fall back to an ambiguous
// repo-unique-name guess after the resolver declines.
func BlocksRepoFallback(ctx shared.ResolveContext) bool {
	receiver, _, ok := elixirQualifiedCall(ctx.Call)
	if !ok {
		return false
	}
	return elixirAliasModuleName(ctx.FileData, receiver) != ""
}

func elixirImportedModuleOwnsFile(ctx shared.ResolveContext, moduleName string, calleeFile string) bool {
	calleeFile = shared.NormalizePath(calleeFile)
	if calleeFile == "" {
		return false
	}
	candidateFiles := []string{calleeFile}
	if root := shared.RepositoryRoot(ctx.RawPath, ctx.RelativePath); root != "" && !filepath.IsAbs(calleeFile) {
		candidateFiles = append(candidateFiles, shared.NormalizePath(filepath.Join(root, calleeFile)))
	}
	for _, path := range ctx.RepositoryImports[moduleName] {
		normalizedPath := shared.NormalizePath(path)
		for _, candidate := range candidateFiles {
			if normalizedPath == candidate {
				return true
			}
		}
	}
	return false
}
