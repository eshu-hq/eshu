// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

type codeCallSymbolKey struct {
	key    string
	method codeprovenance.Method
}

type codeCallSymbolResolution struct {
	entityID string
	method   codeprovenance.Method
}

func addCodeCallSymbolCandidates(
	candidates map[string]map[string]codeCallSymbolResolution,
	item map[string]any,
	entityID string,
) {
	if entityID == "" {
		return
	}
	for _, symbolKey := range codeCallDefinitionSymbolKeys(item) {
		if symbolKey.key == "" {
			continue
		}
		if _, ok := candidates[symbolKey.key]; !ok {
			candidates[symbolKey.key] = map[string]codeCallSymbolResolution{}
		}
		current := candidates[symbolKey.key][entityID]
		candidates[symbolKey.key][entityID] = codeCallSymbolResolution{
			entityID: entityID,
			method:   strongerCodeCallSymbolMethod(current.method, symbolKey.method),
		}
	}
}

func uniqueCodeCallSymbolCandidates(
	candidates map[string]map[string]codeCallSymbolResolution,
) map[string]codeCallSymbolResolution {
	unique := make(map[string]codeCallSymbolResolution, len(candidates))
	for key, entityCandidates := range candidates {
		if len(entityCandidates) != 1 {
			continue
		}
		for _, candidate := range entityCandidates {
			unique[key] = candidate
		}
	}
	return unique
}

func codeCallDefinitionSymbolKeys(item map[string]any) []codeCallSymbolKey {
	keys := make([]codeCallSymbolKey, 0, 4)
	appendKey := func(value any, method codeprovenance.Method) {
		key := strings.TrimSpace(anyToString(value))
		if key == "" {
			return
		}
		keys = append(keys, codeCallSymbolKey{key: key, method: method})
	}

	appendKey(item["scip_symbol"], codeprovenance.MethodSCIP)
	appendKey(item["scip_symbol_key"], codeprovenance.MethodSCIP)
	appendKey(item["scip_moniker"], codeprovenance.MethodSCIP)
	if symbol := strings.TrimSpace(anyToString(item["symbol"])); strings.HasPrefix(symbol, "scip-") {
		appendKey(symbol, codeprovenance.MethodSCIP)
	}

	appendKey(item["package_export_symbol"], codeprovenance.MethodImportBinding)
	appendKey(item["export_symbol"], codeprovenance.MethodImportBinding)
	if stableKey := strings.TrimSpace(anyToString(item["stable_symbol_key"])); stableKey != "" {
		appendKey(stableKey, classifyCodeCallSymbolMethod(stableKey))
	}
	if derived := packageExportSymbolKey(item); derived != "" {
		appendKey(derived, codeprovenance.MethodImportBinding)
	}
	return dedupeCodeCallSymbolKeys(keys)
}

func resolveCodeSymbolCallee(
	index codeEntityIndex,
	edge map[string]any,
) (string, string, codeprovenance.Method) {
	for _, symbolKey := range codeCallEdgeSymbolKeys(edge) {
		resolution := index.entityByStableSymbolKey[symbolKey.key]
		if resolution.entityID == "" {
			continue
		}
		method := resolution.method
		if symbolKey.method == codeprovenance.MethodSCIP {
			method = codeprovenance.MethodSCIP
		}
		return resolution.entityID, index.entityFileByID[resolution.entityID], method
	}
	return "", "", ""
}

func codeCallEdgeSymbolKeys(edge map[string]any) []codeCallSymbolKey {
	keys := make([]codeCallSymbolKey, 0, 4)
	appendKey := func(value any, method codeprovenance.Method) {
		key := strings.TrimSpace(anyToString(value))
		if key == "" {
			return
		}
		keys = append(keys, codeCallSymbolKey{key: key, method: method})
	}

	for _, field := range []string{"callee_symbol", "target_symbol"} {
		if key := strings.TrimSpace(anyToString(edge[field])); key != "" {
			appendKey(key, classifyCodeCallSymbolMethod(key))
		}
	}
	appendKey(edge["package_export_symbol"], codeprovenance.MethodImportBinding)
	appendKey(edge["export_symbol"], codeprovenance.MethodImportBinding)
	if stableKey := strings.TrimSpace(anyToString(edge["stable_symbol_key"])); stableKey != "" {
		appendKey(stableKey, classifyCodeCallSymbolMethod(stableKey))
	}
	if derived := packageExportSymbolKey(edge); derived != "" {
		appendKey(derived, codeprovenance.MethodImportBinding)
	}
	return dedupeCodeCallSymbolKeys(keys)
}

func codeCallReferencedSymbolKeys(envelopes []facts.Envelope) []string {
	keys := make([]codeCallSymbolKey, 0)
	for _, env := range envelopes {
		if env.FactKind != factKindFile {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		for _, bucket := range []string{"function_calls_scip", "function_calls"} {
			for _, edge := range mapSlice(fileData[bucket]) {
				keys = append(keys, codeCallEdgeSymbolKeys(edge)...)
			}
		}
	}
	deduped := dedupeCodeCallSymbolKeys(keys)
	symbolKeys := make([]string, 0, len(deduped))
	for _, key := range deduped {
		symbolKeys = append(symbolKeys, key.key)
	}
	return symbolKeys
}

func packageExportSymbolKey(item map[string]any) string {
	packageID := strings.TrimSpace(anyToString(item["package_id"]))
	exportName := strings.TrimSpace(anyToString(item["export_name"]))
	if exportName == "" {
		exportName = strings.TrimSpace(anyToString(item["exported_name"]))
	}
	if packageID == "" || exportName == "" {
		return ""
	}
	return "package:" + packageID + "#" + exportName
}

func classifyCodeCallSymbolMethod(key string) codeprovenance.Method {
	switch {
	case strings.HasPrefix(key, "scip-"):
		return codeprovenance.MethodSCIP
	default:
		return codeprovenance.MethodImportBinding
	}
}

func strongerCodeCallSymbolMethod(
	left codeprovenance.Method,
	right codeprovenance.Method,
) codeprovenance.Method {
	if left == codeprovenance.MethodSCIP || right == codeprovenance.MethodSCIP {
		return codeprovenance.MethodSCIP
	}
	if left != "" {
		return left
	}
	return right
}

func dedupeCodeCallSymbolKeys(keys []codeCallSymbolKey) []codeCallSymbolKey {
	indexByKey := map[string]int{}
	deduped := make([]codeCallSymbolKey, 0, len(keys))
	for _, key := range keys {
		if key.key == "" {
			continue
		}
		if index, ok := indexByKey[key.key]; ok {
			deduped[index].method = strongerCodeCallSymbolMethod(deduped[index].method, key.method)
			continue
		}
		indexByKey[key.key] = len(deduped)
		deduped = append(deduped, key)
	}
	return deduped
}
