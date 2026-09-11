// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"haskell",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveHaskellQualifiedImportCallee,
		},
	)
}

func resolveHaskellQualifiedImportCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	moduleNames, methodName, ok := haskellQualifiedImportTargets(ctx)
	if !ok || len(moduleNames) != 1 || ctx.repositoryID == "" {
		return "", "", ""
	}
	paths := ctx.repositoryImports[moduleNames[0]]
	if len(paths) == 0 {
		return "", "", ""
	}

	var resolvedEntityID string
	for _, path := range paths {
		path = normalizeCodeCallPath(path)
		if path == "" {
			continue
		}
		entityID := ctx.index.UniqueNameByPath[path][methodName]
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

func haskellQualifiedImportTargets(ctx codeCallResolveContext) ([]string, string, bool) {
	receiver, methodName, ok := haskellQualifiedCall(ctx.call)
	if !ok {
		return nil, "", false
	}
	moduleNames := haskellImportedModuleNames(ctx.fileData, receiver)
	if len(moduleNames) == 0 {
		return nil, "", false
	}
	return moduleNames, methodName, true
}

func haskellQualifiedImportTargetExists(ctx codeCallResolveContext) bool {
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
	for _, entry := range mapSlice(fileData["imports"]) {
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
