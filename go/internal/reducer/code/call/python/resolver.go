// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers returns the Python language resolver list.
// Each call returns a fresh slice, so no caller can mutate another's view.
func Resolvers() []shared.Resolver {
	return []shared.Resolver{
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolvePythonDeclaredBaseCallee,
		},
	}
}

func resolvePythonDeclaredBaseCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	receiver, method, ok := pythonQualifiedClassMethod(ctx.Call)
	if !ok {
		return "", "", ""
	}
	receiverNames := pythonClassBaseCandidateNames(receiver)
	entityID, ambiguous := resolvePythonDirectClassMethod(ctx, receiverNames, method)
	if ambiguous {
		return "", "", ""
	}
	if entityID != "" {
		return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodTypeInferred
	}
	entityID = resolvePythonInheritedClassMethod(ctx, receiverNames, method)
	if entityID == "" {
		return "", "", ""
	}
	return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodTypeInferred
}

func pythonQualifiedClassMethod(call map[string]any) (string, string, bool) {
	fullName := strings.TrimSpace(payloadcore.AnyToString(call["full_name"]))
	if !shared.PythonQualifiedClassReceiver(fullName) {
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
	ctx shared.ResolveContext,
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

func resolvePythonInheritedClassMethod(ctx shared.ResolveContext, receiverNames []string, method string) string {
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
		for _, base := range ctx.Index.PythonClassBasesByRepo(ctx.RepositoryID, className) {
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

func resolvePythonClassMethod(ctx shared.ResolveContext, className string, method string) string {
	className = strings.TrimSpace(className)
	method = strings.TrimSpace(method)
	if ctx.RepositoryID == "" || className == "" || method == "" {
		return ""
	}
	return ctx.Index.UniqueNameByRepo(ctx.RepositoryID, className+"."+method)
}

func pythonClassBaseCandidateNames(base string) []string {
	base = strings.TrimSpace(base)
	if base == "" {
		return nil
	}
	trailing := shared.TrailingName(base)
	if trailing == "" || trailing == base {
		return []string{base}
	}
	return []string{base, trailing}
}
