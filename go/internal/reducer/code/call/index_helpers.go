// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

func spanWidth(span codeFunctionSpan) int {
	return span.endLine - span.startLine
}

func codeCallSpanMatchesAnyName(span codeFunctionSpan, names []string) bool {
	for _, spanName := range span.names {
		for _, name := range names {
			if spanName == name {
				return true
			}
		}
	}
	return false
}

func resolveConstructorMethodCalleeID(index EntityIndex, calleeFilePath string, edge map[string]any) string {
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
