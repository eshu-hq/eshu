// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

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
		key := strings.TrimSpace(payloadcore.AnyToString(value))
		if key == "" {
			return
		}
		keys = append(keys, codeCallSymbolKey{key: key, method: method})
	}

	appendKey(item["scip_symbol"], codeprovenance.MethodSCIP)
	appendKey(item["scip_symbol_key"], codeprovenance.MethodSCIP)
	appendKey(item["scip_moniker"], codeprovenance.MethodSCIP)
	if symbol := strings.TrimSpace(payloadcore.AnyToString(item["symbol"])); strings.HasPrefix(symbol, "scip-") {
		appendKey(symbol, codeprovenance.MethodSCIP)
	}

	appendKey(item["package_export_symbol"], codeprovenance.MethodImportBinding)
	appendKey(item["export_symbol"], codeprovenance.MethodImportBinding)
	if stableKey := strings.TrimSpace(payloadcore.AnyToString(item["stable_symbol_key"])); stableKey != "" {
		appendKey(stableKey, classifyCodeCallSymbolMethod(stableKey))
	}
	if derived := packageExportSymbolKey(item); derived != "" {
		appendKey(derived, codeprovenance.MethodImportBinding)
	}
	return dedupeCodeCallSymbolKeys(keys)
}

// ResolveSymbolCallee resolves a SCIP or import-binding edge to its callee
// entity via the index's stable symbol key table.
func ResolveSymbolCallee(
	index EntityIndex,
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
		key := strings.TrimSpace(payloadcore.AnyToString(value))
		if key == "" {
			return
		}
		keys = append(keys, codeCallSymbolKey{key: key, method: method})
	}

	for _, field := range []string{"callee_symbol", "target_symbol"} {
		if key := strings.TrimSpace(payloadcore.AnyToString(edge[field])); key != "" {
			appendKey(key, classifyCodeCallSymbolMethod(key))
		}
	}
	appendKey(edge["package_export_symbol"], codeprovenance.MethodImportBinding)
	appendKey(edge["export_symbol"], codeprovenance.MethodImportBinding)
	if stableKey := strings.TrimSpace(payloadcore.AnyToString(edge["stable_symbol_key"])); stableKey != "" {
		appendKey(stableKey, classifyCodeCallSymbolMethod(stableKey))
	}
	if derived := packageExportSymbolKey(edge); derived != "" {
		appendKey(derived, codeprovenance.MethodImportBinding)
	}
	return dedupeCodeCallSymbolKeys(keys)
}

// ReferencedSymbolKeys scans "file" facts in envelopes for SCIP and
// import-binding call edges (function_calls_scip and function_calls) and
// returns the deduplicated set of symbol keys they reference. Callers use this
// to load only the symbol-definition facts a generation actually needs before
// resolving call edges via [BuildEntityIndex]'s entityByStableSymbolKey.
func ReferencedSymbolKeys(envelopes []facts.Envelope) []string {
	keys := make([]codeCallSymbolKey, 0)
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindFile {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		for _, bucket := range []string{"function_calls_scip", "function_calls"} {
			for _, edge := range payloadcore.MapSlice(fileData[bucket]) {
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
	packageID := strings.TrimSpace(payloadcore.AnyToString(item["package_id"]))
	exportName := strings.TrimSpace(payloadcore.AnyToString(item["export_name"]))
	if exportName == "" {
		exportName = strings.TrimSpace(payloadcore.AnyToString(item["exported_name"]))
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

// SamePackageGoCalleeKeysForDeltaRepos derives stable symbol keys for
// keyless same-package Go calls on delta-generation repositories, so the
// active symbol-definition load can fetch the unchanged files' definitions
// those calls resolve against.
//
// The tree-sitter Go parser emits a same-package call as a name-only
// function_calls edge: no callee_symbol, no import binding, nothing
// ReferencedSymbolKeys can carry to the overlay. On a full generation that
// is fine — every definition shares the index, so the repo-unique-name
// fallback resolves the call. On a delta generation only the changed files'
// facts are loaded, so the fallback finds nothing and the row drops as an
// unresolved callee while the file-scoped refresh still retracts its old
// edge (#10049 delta-reader Reds: the three cross-file CALLS).
//
// The derived key reuses the exact shape the Go parser stamps on a
// definition: "scip-go gomod <package import path> <Name>().". It only fires
// when the call is provably same-package: a bare unqualified Go name in a
// file whose imports carry no local binding. A dot import (recorded with its
// "." alias since #10301) pulls another package's exported names into the
// bare namespace, and any other alias can shadow a bare name, so a file with
// an aliased import keeps today's behavior: no derived keys. A derived key
// that matches nothing (a builtin, a method, a removed callee) loads nothing
// and resolves nothing, exactly as today.
//
// Callers pass only delta-generation repository IDs: full generations keep
// their keyless path byte-identical.
func SamePackageGoCalleeKeysForDeltaRepos(
	envelopes []facts.Envelope,
	deltaRepos map[string]struct{},
) []string {
	if len(deltaRepos) == 0 {
		return nil
	}
	var keys []string
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindFile {
			continue
		}
		if _, onDelta := deltaRepos[payloadcore.PayloadStr(env.Payload, "repo_id")]; !onDelta {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		importPath := strings.TrimSpace(payloadcore.AnyToString(fileData["package_import_path"]))
		if importPath == "" {
			continue
		}
		if goFileHasImportAlias(fileData) {
			continue
		}
		for _, edge := range payloadcore.MapSlice(fileData["function_calls"]) {
			if len(codeCallEdgeSymbolKeys(edge)) > 0 {
				continue
			}
			if strings.TrimSpace(payloadcore.AnyToString(edge["lang"])) != "go" {
				continue
			}
			name := strings.TrimSpace(payloadcore.AnyToString(edge["full_name"]))
			if name == "" || name != strings.TrimSpace(payloadcore.AnyToString(edge["name"])) {
				continue
			}
			if strings.ContainsAny(name, "./ \t\n") || strings.HasPrefix(name, "scip-") {
				continue
			}
			keys = append(keys, "scip-go gomod "+importPath+" "+name+"().")
		}
	}
	if len(keys) == 0 {
		return nil
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// goFileHasImportAlias reports whether any entry in the file's imports
// bucket binds a local name: a named alias, or the blank ("_") or dot (".")
// binding the Go parser records since #10301. Such a file's bare call names
// are not provably same-package — a dot import pulls another package's names
// into the bare namespace and any alias can shadow a bare name — so the
// caller skips derivation for the whole file, the fail-safe behavior. Files
// with only plain imports (no alias key) derive normally.
func goFileHasImportAlias(fileData map[string]any) bool {
	for _, entry := range payloadcore.MapSlice(fileData["imports"]) {
		if strings.TrimSpace(payloadcore.AnyToString(entry["alias"])) != "" {
			return true
		}
	}
	return false
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
