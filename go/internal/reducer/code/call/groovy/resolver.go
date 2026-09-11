// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package groovy

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers returns the Groovy language resolver list.
// Each call returns a fresh slice, so no caller can mutate another's view.
func Resolvers() []shared.Resolver {
	return []shared.Resolver{
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveGroovyClassQualifiedCallee,
		},
	}
}

func resolveGroovyClassQualifiedCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	for _, candidateName := range groovyClassQualifiedCandidateNames(ctx.Call) {
		entityID := ctx.Index.UniqueNameByRepo[ctx.RepositoryID][candidateName]
		if entityID == "" {
			continue
		}
		return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodTypeInferred
	}
	return "", "", ""
}

func groovyClassQualifiedCandidateNames(call map[string]any) []string {
	callName := strings.TrimSpace(payloadcore.AnyToString(call["name"]))
	receiverType := strings.TrimSpace(payloadcore.AnyToString(call["inferred_obj_type"]))
	if callName == "" || receiverType == "" {
		return nil
	}
	candidates := []string{receiverType + "." + callName}
	if fullName := strings.TrimSpace(payloadcore.AnyToString(call["full_name"])); strings.HasSuffix(fullName, "."+callName) {
		candidates = append(candidates, fullName)
	}
	return candidates
}
