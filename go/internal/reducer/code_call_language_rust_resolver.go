// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"rust",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveRustTraitBoundReceiverCallee,
		},
	)
}

func resolveRustTraitBoundReceiverCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	methodName := ctx.callName()
	receiverType := strings.TrimSpace(anyToString(ctx.call["inferred_obj_type"]))
	if methodName == "" || receiverType == "" || !rustReceiverMethodCall(ctx.call) {
		return "", "", ""
	}
	caller := rustContainingFunctionItem(ctx)
	if caller == nil {
		return "", "", ""
	}
	matches := map[string]struct{}{}
	for _, traitName := range rustTraitBoundsForType(caller, receiverType) {
		for _, candidate := range rustTraitNameCandidates(traitName) {
			if entityID := ctx.index.rustTraitMethodsByRepo[ctx.repositoryID][candidate+"::"+methodName]; entityID != "" {
				matches[entityID] = struct{}{}
			}
		}
	}
	if len(matches) != 1 {
		return "", "", ""
	}
	for entityID := range matches {
		return entityID, ctx.index.entityFileByID[entityID], codeprovenance.MethodTypeInferred
	}
	return "", "", ""
}

func rustReceiverMethodCall(call map[string]any) bool {
	fullName := strings.TrimSpace(anyToString(call["full_name"]))
	dot := strings.LastIndex(fullName, ".")
	return dot > 0 && dot < len(fullName)-1 && !strings.Contains(fullName[:dot], "::")
}

func rustContainingFunctionItem(ctx codeCallResolveContext) map[string]any {
	line := codeCallInt(ctx.call["line_number"], ctx.call["ref_line"])
	if line <= 0 {
		return nil
	}
	var (
		best      map[string]any
		bestWidth int
	)
	for _, item := range mapSlice(ctx.fileData["functions"]) {
		startLine := codeCallInt(item["line_number"], item["start_line"])
		endLine := codeCallInt(item["end_line"])
		if startLine <= 0 {
			continue
		}
		if endLine < startLine {
			endLine = startLine
		}
		if line < startLine || line > endLine {
			continue
		}
		width := endLine - startLine
		if best == nil || width < bestWidth {
			best = item
			bestWidth = width
		}
	}
	return best
}

func rustTraitBoundsForType(function map[string]any, receiverType string) []string {
	receiverType = strings.TrimSpace(receiverType)
	if receiverType == "" {
		return nil
	}
	out := make([]string, 0)
	for _, predicate := range codeCallMetadataStringSlice(function, "where_predicates") {
		subject, bounds, ok := rustWherePredicateParts(predicate)
		if !ok || strings.TrimSpace(subject) != receiverType {
			continue
		}
		for _, bound := range strings.Split(bounds, "+") {
			traitName := rustTraitBoundName(bound)
			if traitName != "" {
				out = appendUniqueString(out, traitName)
			}
		}
	}
	return out
}

func rustWherePredicateParts(predicate string) (string, string, bool) {
	for idx, r := range predicate {
		if r != ':' {
			continue
		}
		if idx > 0 && predicate[idx-1] == ':' {
			continue
		}
		if idx+1 < len(predicate) && predicate[idx+1] == ':' {
			continue
		}
		return strings.TrimSpace(predicate[:idx]), strings.TrimSpace(predicate[idx+1:]), true
	}
	return "", "", false
}

func rustTraitBoundName(bound string) string {
	traitName := strings.TrimSpace(bound)
	traitName = strings.TrimPrefix(traitName, "?")
	traitName = strings.TrimSpace(traitName)
	if traitName == "" || strings.Contains(traitName, "=") || strings.HasPrefix(traitName, "for<") {
		return ""
	}
	return traitName
}

func rustTraitNameCandidates(traitName string) []string {
	traitName = strings.TrimSpace(traitName)
	if traitName == "" {
		return nil
	}
	trailing := codeCallTrailingName(traitName)
	if trailing == "" || trailing == traitName {
		return []string{traitName}
	}
	return []string{traitName, trailing}
}
