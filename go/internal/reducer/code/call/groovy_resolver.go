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
		"groovy",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveGroovyClassQualifiedCallee,
		},
	)
}

func resolveGroovyClassQualifiedCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	for _, candidateName := range groovyClassQualifiedCandidateNames(ctx.call) {
		entityID := ctx.index.UniqueNameByRepo[ctx.repositoryID][candidateName]
		if entityID == "" {
			continue
		}
		return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
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
