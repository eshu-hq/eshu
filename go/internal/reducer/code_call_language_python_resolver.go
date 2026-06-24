// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"python",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolvePythonDeclaredBaseCallee,
		},
	)
}

func resolvePythonDeclaredBaseCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	receiver, method, ok := pythonQualifiedClassMethod(ctx.call)
	if !ok {
		return "", "", ""
	}
	receiverNames := pythonClassBaseCandidateNames(receiver)
	entityID, ambiguous := resolvePythonDirectClassMethod(ctx, receiverNames, method)
	if ambiguous {
		return "", "", ""
	}
	if entityID != "" {
		return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
	}
	entityID = resolvePythonInheritedClassMethod(ctx, receiverNames, method)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
}

func pythonQualifiedClassMethod(call map[string]any) (string, string, bool) {
	fullName := strings.TrimSpace(anyToString(call["full_name"]))
	if !codeCallPythonQualifiedClassReceiver(fullName) {
		return "", "", false
	}
	dot := strings.LastIndex(fullName, ".")
	if dot <= 0 || dot >= len(fullName)-1 {
		return "", "", false
	}
	receiver := strings.TrimSpace(fullName[:dot])
	method := strings.TrimSpace(fullName[dot+1:])
	return receiver, method, receiver != "" && method != ""
}

func resolvePythonDirectClassMethod(
	ctx codeCallResolveContext,
	receiverNames []string,
	method string,
) (string, bool) {
	matches := map[string]struct{}{}
	for _, receiverName := range receiverNames {
		if entityID := resolvePythonClassMethod(ctx, receiverName, method); entityID != "" {
			matches[entityID] = struct{}{}
		}
	}
	if len(matches) > 1 {
		return "", true
	}
	return uniquePythonResolvedEntity(matches), false
}

func resolvePythonInheritedClassMethod(ctx codeCallResolveContext, receiverNames []string, method string) string {
	seenClasses := map[string]struct{}{}
	matches := map[string]struct{}{}
	var walk func(string)
	walk = func(className string) {
		if className == "" {
			return
		}
		if _, ok := seenClasses[className]; ok {
			return
		}
		seenClasses[className] = struct{}{}
		for _, base := range ctx.index.pythonClassBasesByRepo[ctx.repositoryID][className] {
			for _, baseName := range pythonClassBaseCandidateNames(base) {
				if entityID := resolvePythonClassMethod(ctx, baseName, method); entityID != "" {
					matches[entityID] = struct{}{}
				}
				walk(baseName)
			}
		}
	}
	for _, receiverName := range receiverNames {
		walk(receiverName)
	}
	return uniquePythonResolvedEntity(matches)
}

func uniquePythonResolvedEntity(matches map[string]struct{}) string {
	if len(matches) != 1 {
		return ""
	}
	for entityID := range matches {
		return entityID
	}
	return ""
}

func resolvePythonClassMethod(ctx codeCallResolveContext, className string, method string) string {
	className = strings.TrimSpace(className)
	method = strings.TrimSpace(method)
	if ctx.repositoryID == "" || className == "" || method == "" {
		return ""
	}
	return ctx.index.uniqueNameByRepo[ctx.repositoryID][className+"."+method]
}

func pythonClassBaseCandidateNames(base string) []string {
	base = strings.TrimSpace(base)
	if base == "" {
		return nil
	}
	trailing := codeCallTrailingName(base)
	if trailing == "" || trailing == base {
		return []string{base}
	}
	return []string{base, trailing}
}
