// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

var (
	javaScriptStringMemberRe   = regexp.MustCompile(`\[\s*["']([^"']+)["']\s*\]`)
	javaScriptVariableMemberRe = regexp.MustCompile(`\[\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\]`)
)

// ResolveDynamicCallee handles static JavaScript patterns that look dynamic
// in call metadata but have a literal same-file target.
func ResolveDynamicCallee(
	index shared.EntityIndex,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	call map[string]any,
) string {
	if !shared.JavaScriptFamily(shared.CallLanguage(call, rawPath, relativePath)) {
		return ""
	}
	callLine := shared.PayloadInt(call["line_number"], call["ref_line"])
	if callLine <= 0 {
		return ""
	}

	// JavaScript alias metadata should normally come from the index; the
	// fallback preserves direct helper tests that bypass index construction.
	aliasSet, ok := javaScriptStaticAliasesForCall(index, rawPath, relativePath, callLine)
	if !ok {
		source := shared.JavaScriptContainingFunctionSource(fileData, callLine)
		if strings.TrimSpace(source) == "" {
			return ""
		}
		aliases, staticStrings := shared.JavaScriptStaticAliases(source)
		aliasSet = shared.JavaScriptAliasSet{
			Aliases:       aliases,
			StaticStrings: staticStrings,
			Scanned:       true,
		}
	}
	for _, candidate := range javaScriptDynamicCallCandidates(call, aliasSet.StaticStrings) {
		if strings.ContainsAny(candidate, "[]") {
			continue
		}
		target := candidate
		if aliasTarget := aliasSet.Aliases[candidate]; aliasTarget != "" {
			target = aliasTarget
		}
		if entityID := resolveSameFileJavaScriptDynamicTarget(index, rawPath, relativePath, target); entityID != "" {
			return entityID
		}
	}
	return ""
}

func javaScriptStaticAliasesForCall(
	index shared.EntityIndex,
	rawPath string,
	relativePath string,
	line int,
) (shared.JavaScriptAliasSet, bool) {
	var (
		bestAlias shared.JavaScriptAliasSet
		bestWidth int
	)
	for _, pathKey := range shared.PathKeys(rawPath, relativePath) {
		for _, span := range index.JavaScriptAliasesByPath(pathKey) {
			if line < span.StartLine || line > span.EndLine {
				continue
			}
			width := span.EndLine - span.StartLine
			if !bestAlias.Scanned || width < bestWidth {
				bestAlias = span.Aliases
				bestWidth = width
			}
		}
		if bestAlias.Scanned {
			return bestAlias, true
		}
	}
	return shared.JavaScriptAliasSet{}, false
}

func javaScriptDynamicCallCandidates(call map[string]any, staticStrings map[string]string) []string {
	candidates := make([]string, 0, 6)
	appendCandidate := func(value string) {
		value = javaScriptNormalizeStaticMemberExpression(value, staticStrings)
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}

	fullName := payloadcore.AnyToString(call["full_name"])
	appendCandidate(fullName)
	appendCandidate(payloadcore.AnyToString(call["name"]))
	for _, receiver := range shared.JavaScriptFunctionReceiverNames(fullName) {
		appendCandidate(receiver)
	}
	return candidates
}

func javaScriptNormalizeStaticMemberExpression(value string, staticStrings map[string]string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = javaScriptStringMemberRe.ReplaceAllString(value, ".$1")
	return javaScriptVariableMemberRe.ReplaceAllStringFunc(value, func(match string) string {
		parts := javaScriptVariableMemberRe.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		if resolved := staticStrings[parts[1]]; resolved != "" {
			return "." + resolved
		}
		return match
	})
}

func resolveSameFileJavaScriptDynamicTarget(
	index shared.EntityIndex,
	rawPath string,
	relativePath string,
	target string,
) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.ContainsAny(target, "[]") {
		return ""
	}
	candidates := []string{target, shared.TrailingName(target)}
	for _, pathKey := range shared.PathKeys(rawPath, relativePath) {
		for _, candidate := range candidates {
			if entityID := index.UniqueNameByPath(pathKey, candidate); entityID != "" {
				return entityID
			}
		}
	}
	return ""
}
