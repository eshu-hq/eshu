// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package haskell

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers returns the Haskell language resolver list.
// Each call returns a fresh slice, so no caller can mutate another's view.
func Resolvers() []shared.Resolver {
	return []shared.Resolver{
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveHaskellQualifiedImportCallee,
		},
	}
}

func resolveHaskellQualifiedImportCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	moduleNames, methodName, ok := haskellQualifiedImportTargets(ctx)
	if !ok || len(moduleNames) != 1 || ctx.RepositoryID == "" {
		return "", "", ""
	}
	paths := ctx.RepositoryImports[moduleNames[0]]
	if len(paths) == 0 {
		return "", "", ""
	}

	var resolvedEntityID string
	for _, path := range paths {
		path = shared.NormalizePath(path)
		if path == "" {
			continue
		}
		entityID := ctx.Index.UniqueNameByPath(path, methodName)
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

func haskellQualifiedImportTargets(ctx shared.ResolveContext) ([]string, string, bool) {
	receiver, methodName, ok := haskellQualifiedCall(ctx.Call)
	if !ok {
		return nil, "", false
	}
	moduleNames := haskellImportedModuleNames(ctx.FileData, receiver)
	if len(moduleNames) == 0 {
		return nil, "", false
	}
	return moduleNames, methodName, true
}

// QualifiedImportTargetExists reports whether the Haskell file's imports
// explain (but may fail to uniquely resolve) a qualified call, so the
// dispatch must not fall back to an ambiguous repo-unique-name guess after
// the resolver declines.
func QualifiedImportTargetExists(ctx shared.ResolveContext) bool {
	moduleNames, _, ok := haskellQualifiedImportTargets(ctx)
	return ok && len(moduleNames) > 0
}

func haskellQualifiedCall(call map[string]any) (string, string, bool) {
	fullName := strings.TrimSpace(payloadcore.AnyToString(call["full_name"]))
	dot := strings.LastIndex(fullName, ".")
	if dot <= 0 || dot >= len(fullName)-1 {
		return "", "", false
	}
	receiver := strings.TrimSpace(fullName[:dot])
	methodName := strings.TrimSpace(fullName[dot+1:])
	return receiver, methodName, receiver != "" && methodName != ""
}

func haskellImportedModuleNames(fileData map[string]any, receiver string) []string {
	receiver = strings.TrimSpace(receiver)
	if receiver == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var moduleNames []string
	for _, entry := range payloadcore.MapSlice(fileData["imports"]) {
		if lang := strings.TrimSpace(payloadcore.AnyToString(entry["lang"])); lang != "" && lang != "haskell" {
			continue
		}
		moduleName := strings.TrimSpace(payloadcore.AnyToString(entry["name"]))
		if moduleName == "" {
			continue
		}
		alias := strings.TrimSpace(payloadcore.AnyToString(entry["alias"]))
		if alias == "" {
			alias = moduleName
		}
		if receiver == alias || receiver == moduleName {
			if _, ok := seen[moduleName]; ok {
				continue
			}
			seen[moduleName] = struct{}{}
			moduleNames = append(moduleNames, moduleName)
		}
	}
	return moduleNames
}
