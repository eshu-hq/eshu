// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package typescript

import (
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers returns the TypeScript/TSX language resolver list. code/call wires it
// under both the "typescript" and "tsx" language keys, matching the
// pre-split dual registration.
// Each call returns a fresh slice, so no caller can mutate another's view.
func Resolvers() []shared.Resolver {
	return []shared.Resolver{
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveTypeScriptInterfaceCallee,
		},
	}
}

func resolveTypeScriptInterfaceCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	interfaceName := typeScriptSimpleInterfaceName(payloadcore.AnyToString(ctx.Call["inferred_obj_type"]))
	methodName := ctx.CallName()
	if ctx.RepositoryID == "" || interfaceName == "" || methodName == "" {
		return "", "", ""
	}
	entityID := ctx.Index.TypeScriptInterfaceMethodsByRepo(ctx.RepositoryID, interfaceName, methodName)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodTypeInferred
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
