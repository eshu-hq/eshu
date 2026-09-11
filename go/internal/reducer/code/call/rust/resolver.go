// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers returns the Rust language resolver list.
// Each call returns a fresh slice, so no caller can mutate another's view.
func Resolvers() []shared.Resolver {
	return []shared.Resolver{
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveRustTraitBoundReceiverCallee,
		},
	}
}

func resolveRustTraitBoundReceiverCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	methodName := ctx.CallName()
	receiverType := strings.TrimSpace(payloadcore.AnyToString(ctx.Call["inferred_obj_type"]))
	if methodName == "" || receiverType == "" || !rustReceiverMethodCall(ctx.Call) {
		return "", "", ""
	}
	caller := rustContainingFunctionItem(ctx)
	if caller == nil {
		return "", "", ""
	}
	matches := map[string]struct{}{}
	for _, traitName := range rustTraitBoundsForType(caller, receiverType) {
		for _, candidate := range rustTraitNameCandidates(traitName) {
			if entityID := ctx.Index.RustTraitMethodsByRepo(ctx.RepositoryID, candidate+"::"+methodName); entityID != "" {
				matches[entityID] = struct{}{}
			}
		}
	}
	if len(matches) != 1 {
		return "", "", ""
	}
	for entityID := range matches {
		return entityID, ctx.Index.EntityFileByID(entityID), codeprovenance.MethodTypeInferred
	}
	return "", "", ""
}

func rustReceiverMethodCall(call map[string]any) bool {
	fullName := strings.TrimSpace(payloadcore.AnyToString(call["full_name"]))
	dot := strings.LastIndex(fullName, ".")
	return dot > 0 && dot < len(fullName)-1 && !strings.Contains(fullName[:dot], "::")
}

func rustContainingFunctionItem(ctx shared.ResolveContext) map[string]any {
	line := shared.PayloadInt(ctx.Call["line_number"], ctx.Call["ref_line"])
	if line <= 0 {
		return nil
	}
	var (
		best      map[string]any
		bestWidth int
	)
	for _, item := range payloadcore.MapSlice(ctx.FileData["functions"]) {
		startLine := shared.PayloadInt(item["line_number"], item["start_line"])
		endLine := shared.PayloadInt(item["end_line"])
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
	for _, predicate := range shared.MetadataStringSlice(function, "where_predicates") {
		subject, bounds, ok := rustWherePredicateParts(predicate)
		if !ok || strings.TrimSpace(subject) != receiverType {
			continue
		}
		for _, bound := range strings.Split(bounds, "+") {
			traitName := rustTraitBoundName(bound)
			if traitName != "" {
				out = payloadcore.AppendUniqueString(out, traitName)
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
	trailing := shared.TrailingName(traitName)
	if trailing == "" || trailing == traitName {
		return []string{traitName}
	}
	return []string{traitName, trailing}
}
