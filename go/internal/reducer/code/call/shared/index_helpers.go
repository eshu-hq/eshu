// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// SpanWidth returns the line-count width of span (EndLine - StartLine),
// used to prefer the narrowest containing span when several overlap.
func SpanWidth(span FunctionSpan) int {
	return span.EndLine - span.StartLine
}

// SpanMatchesAnyName reports whether span's candidate names include any of
// names.
func SpanMatchesAnyName(span FunctionSpan, names []string) bool {
	for _, spanName := range span.names {
		for _, name := range names {
			if spanName == name {
				return true
			}
		}
	}
	return false
}

// uniqueCodeCallNamesByDirectory keeps only directory-local names with exactly
// one entity candidate.
func uniqueCodeCallNamesByDirectory(
	dirs map[string]map[string]map[string]struct{},
) map[string]map[string]string {
	uniqueNames := make(map[string]map[string]string, len(dirs))
	for dir, names := range dirs {
		uniqueNames[dir] = make(map[string]string, len(names))
		for name, entityIDs := range names {
			if len(entityIDs) != 1 {
				continue
			}
			for entityID := range entityIDs {
				uniqueNames[dir][name] = entityID
			}
		}
	}
	return uniqueNames
}

// ResolveConstructorMethodCalleeID resolves a constructor_call edge to the
// constructor entity declared for its class name within calleeFilePath, or ""
// when edge is not a constructor call or no such constructor is indexed.
func ResolveConstructorMethodCalleeID(index EntityIndex, calleeFilePath string, edge map[string]any) string {
	if payloadcore.AnyToString(edge["call_kind"]) != "constructor_call" {
		return ""
	}
	className := strings.TrimSpace(payloadcore.AnyToString(edge["name"]))
	if className == "" {
		className = strings.TrimSpace(payloadcore.AnyToString(edge["full_name"]))
	}
	if className == "" {
		return ""
	}
	for _, pathKey := range PathKeys(calleeFilePath, "") {
		if entityID := index.constructorByPath[pathKey][className]; entityID != "" {
			return entityID
		}
	}
	return ""
}
