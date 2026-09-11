// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perl

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers is the Perl language resolver list.
var Resolvers = []shared.Resolver{
	{
		Phase:   shared.PhaseBeforeRepoFallback,
		Resolve: resolvePerlPackageImportCallee,
	},
}

func resolvePerlPackageImportCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	packageName, methodName, ok := perlPackageQualifiedCall(ctx.Call)
	if !ok || ctx.RepositoryID == "" {
		return "", "", ""
	}
	if !perlFileImportsPackage(ctx.FileData, packageName) {
		return "", "", ""
	}
	paths := ctx.RepositoryImports[packageName]
	if len(paths) == 0 {
		return "", "", ""
	}

	var resolvedEntityID string
	for _, path := range paths {
		path = shared.NormalizePath(path)
		if path == "" {
			continue
		}
		for _, candidateName := range perlImportedFunctionCandidateNames(packageName, methodName) {
			entityID := ctx.Index.UniqueNameByPath[path][candidateName]
			if entityID == "" || entityID == resolvedEntityID {
				continue
			}
			if resolvedEntityID != "" {
				return "", "", ""
			}
			resolvedEntityID = entityID
		}
	}
	if resolvedEntityID == "" {
		return "", "", ""
	}
	return resolvedEntityID, ctx.Index.EntityFileByID(resolvedEntityID), codeprovenance.MethodImportBinding
}

func perlPackageQualifiedCall(call map[string]any) (string, string, bool) {
	fullName := strings.TrimSpace(payloadcore.AnyToString(call["full_name"]))
	if fullName == "" {
		fullName = strings.TrimSpace(payloadcore.AnyToString(call["name"]))
	}
	separator := strings.LastIndex(fullName, "::")
	if separator <= 0 || separator >= len(fullName)-2 {
		return "", "", false
	}
	packageName := strings.TrimSpace(fullName[:separator])
	methodName := strings.TrimSpace(fullName[separator+2:])
	return packageName, methodName, packageName != "" && methodName != ""
}

func perlFileImportsPackage(fileData map[string]any, packageName string) bool {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" {
		return false
	}
	for _, entry := range payloadcore.MapSlice(fileData["imports"]) {
		if lang := strings.TrimSpace(payloadcore.AnyToString(entry["lang"])); lang != "" && lang != "perl" {
			continue
		}
		if strings.TrimSpace(payloadcore.AnyToString(entry["name"])) == packageName {
			return true
		}
	}
	return false
}

func perlImportedFunctionCandidateNames(packageName string, methodName string) []string {
	methodName = strings.TrimSpace(methodName)
	if methodName == "" {
		return nil
	}
	candidates := []string{strings.TrimSpace(packageName) + "::" + methodName, methodName}
	if receiver := shared.TrailingName(packageName); receiver != "" {
		candidates = append(candidates, receiver+"."+methodName)
	}
	return candidates
}
