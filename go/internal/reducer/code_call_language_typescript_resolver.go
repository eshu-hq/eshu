// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"typescript",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveTypeScriptInterfaceCallee,
		},
	)
	registerCodeCallLanguageResolvers(
		"tsx",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveTypeScriptInterfaceCallee,
		},
	)
}

func resolveTypeScriptInterfaceCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	interfaceName := typeScriptSimpleInterfaceName(anyToString(ctx.call["inferred_obj_type"]))
	methodName := ctx.callName()
	if ctx.repositoryID == "" || interfaceName == "" || methodName == "" {
		return "", "", ""
	}
	entityID := ctx.index.typeScriptInterfaceMethodsByRepo[ctx.repositoryID][interfaceName][methodName]
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
}

func typeScriptSimpleInterfaceName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.ContainsAny(trimmed, "|&<>{}[]().,") {
		return ""
	}
	for _, r := range trimmed {
		if r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return ""
	}
	return trimmed
}
