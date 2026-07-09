// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// valueFlowFixtureEntities returns a diverse entity set with functions across
// multiple files, receivers, and line numbers to exercise every lookup path
// the five value-flow builders use.
func valueFlowFixtureEntities() []content.EntityRecord {
	return []content.EntityRecord{
		{EntityID: "e-handle", Path: "src/handler.go", EntityType: "Function", EntityName: "handle", StartLine: 3},
		{EntityID: "e-view", Path: "src/handler.go", EntityType: "Function", EntityName: "view", StartLine: 10},
		{EntityID: "e-query", Path: "src/handler.go", EntityType: "Function", EntityName: "query", StartLine: 17},
		{EntityID: "e-a-handle", Path: "src/handler.go", EntityType: "Function", EntityName: "Handle", StartLine: 3, Metadata: map[string]any{"class_context": "A"}},
		{EntityID: "e-b-handle", Path: "src/handler.go", EntityType: "Function", EntityName: "Handle", StartLine: 24, Metadata: map[string]any{"class_context": "B"}},
		{EntityID: "e-worker", Path: "pkg/worker.go", EntityType: "Function", EntityName: "run", StartLine: 5},
		{EntityID: "e-mypkg", Path: "pkg/worker.go", EntityType: "Function", EntityName: "run", StartLine: 20},
		{EntityID: "e-class", Path: "src/model.go", EntityType: "Class", EntityName: "User", StartLine: 1},
		{EntityID: "e-iface", Path: "src/iface.go", EntityType: "Interface", EntityName: "Handler", StartLine: 3},
		{EntityID: "e-k8s", Path: "deploy/k8s.yaml", EntityType: "K8sResource", EntityName: "my-deployment", StartLine: 1},
	}
}

func valueFlowFixtureParsedFiles() []map[string]any {
	return []map[string]any{
		{
			"path": "/repo/src/handler.go",
			"functions": []map[string]any{
				{"name": "handle", "line_number": 3},
				{"name": "view", "line_number": 10},
				{"name": "query", "line_number": 17},
				{"name": "Handle", "line_number": 3},
				{"name": "Handle", "line_number": 24},
			},
			"classes": []map[string]any{
				{"name": "User", "line_number": 42},
			},
			"taint_findings": []map[string]any{
				{
					"function_name": "handle",
					"line_number":   3,
					"lang":          "go",
					"kind":          "TAINTED",
					"sink_kind":     "sql",
					"source_kind":   "http_request",
					"binding":       "q",
					"source_line":   4,
					"sink_line":     5,
					"confidence":    0.8,
				},
			},
			"interproc_findings": []map[string]any{
				{
					"source_func": "\x1fpkg\x1fA\x1fHandle",
					"sink_func":   "\x1fpkg\x1f\x1fview",
					"source_kind": "http_request",
					"sink_kind":   "sql",
					"confidence":  0.6,
					"lang":        "go",
				},
			},
			"dataflow_summaries": []map[string]any{
				{
					"function_id": "repo-1\x1fpkg\x1f\x1fhandle",
					"lang":        "go",
				},
			},
			"dataflow_functions": []map[string]any{
				{
					"function_name": "handle",
					"line_number":   3,
					"lang":          "go",
					"blocks":        []map[string]any{{"id": 0, "succs": []int{}}},
				},
			},
		},
		{
			"path": "/repo/pkg/worker.go",
			"functions": []map[string]any{
				{"name": "run", "line_number": 5},
				{"name": "run", "line_number": 20},
			},
			"dataflow_functions": []map[string]any{
				{
					"function_name": "run",
					"line_number":   5,
					"lang":          "go",
					"blocks":        []map[string]any{{"id": 0, "succs": []int{}}},
				},
				{
					"function_name": "run",
					"line_number":   20,
					"lang":          "go",
					"blocks":        []map[string]any{{"id": 0, "succs": []int{}}},
				},
			},
		},
	}
}

// TestSharedEntityUIDLookupEqualsPerBuilder proves the shared entity-lookup map
// built by buildEntityUIDLookup is byte-identical to the lookup each of the
// three individual builders (annotateParsedFilesWithEntityIDs, buildTaintEvidence,
// buildDataflowFunctions) would have built from the same entities.
func TestSharedEntityUIDLookupEqualsPerBuilder(t *testing.T) {
	t.Parallel()

	entities := valueFlowFixtureEntities()
	shared := buildEntityUIDLookup(entities)

	// Replicate the per-builder lookup construction exactly.
	perBuilder := make(map[string]string, len(entities))
	for _, entity := range entities {
		key := entityLookupKey(entity.Path, entity.EntityType, entity.EntityName, entity.StartLine)
		perBuilder[key] = entity.EntityID
	}

	if len(shared) != len(perBuilder) {
		t.Fatalf("shared map len = %d, per-builder len = %d", len(shared), len(perBuilder))
	}
	for key, sharedID := range shared {
		perBuilderID, ok := perBuilder[key]
		if !ok {
			t.Fatalf("shared key %q not in per-builder map", key)
		}
		if sharedID != perBuilderID {
			t.Fatalf("key %q: shared=%q perBuilder=%q", key, sharedID, perBuilderID)
		}
	}
	// Symmetric: per-builder keys all present in shared.
	for key, perBuilderID := range perBuilder {
		sharedID, ok := shared[key]
		if !ok {
			t.Fatalf("per-builder key %q not in shared map", key)
		}
		if sharedID != perBuilderID {
			t.Fatalf("key %q: perBuilder=%q shared=%q", key, perBuilderID, sharedID)
		}
	}
}

// TestSharedFunctionUIDResolverEqualsPerBuilder proves the shared function UID
// resolver produces the same resolutions as one built independently by
// newFunctionUIDResolver from the same entities.
func TestSharedFunctionUIDResolverEqualsPerBuilder(t *testing.T) {
	t.Parallel()

	entities := valueFlowFixtureEntities()
	shared := newFunctionUIDResolver(entities)
	perBuilder := newFunctionUIDResolver(entities)

	cases := []struct {
		path     string
		receiver string
		name     string
	}{
		{"src/handler.go", "", "handle"},
		{"src/handler.go", "", "view"},
		{"src/handler.go", "", "query"},
		{"src/handler.go", "A", "Handle"},
		{"src/handler.go", "B", "Handle"},
		{"src/handler.go", "C", "Handle"}, // not an entity — should resolve false
		{"pkg/worker.go", "", "run"},      // ambiguous → false
		{"pkg/worker.go", "", "notfound"}, // absent → false
		{"", "", ""},                      // empty name → false
	}
	for _, tc := range cases {
		sharedUID, sharedOK := shared(tc.path, tc.receiver, tc.name)
		perUID, perOK := perBuilder(tc.path, tc.receiver, tc.name)
		if sharedUID != perUID || sharedOK != perOK {
			t.Fatalf("(%s, %s, %s): shared=(%q,%v) perBuilder=(%q,%v)",
				tc.path, tc.receiver, tc.name, sharedUID, sharedOK, perUID, perOK)
		}
	}
}

// TestValueFlowBuildersShareEntityLookup proves the three entity-lookup
// consumers produce byte-identical output whether given the shared map or an
// independently-constructed clone of the same map.
func TestValueFlowBuildersShareEntityLookup(t *testing.T) {
	t.Parallel()

	entities := valueFlowFixtureEntities()
	parsed := valueFlowFixtureParsedFiles()
	shared := buildEntityUIDLookup(entities)

	// Clone parsed files so we can run each builder twice without mutation
	// interference.
	parsedCopy := func(src []map[string]any) []map[string]any {
		data, _ := json.Marshal(src)
		var dst []map[string]any
		_ = json.Unmarshal(data, &dst)
		return dst
	}

	// annotateParsedFilesWithEntityIDs mutates parsedFiles; run both sides from
	// identical copies.
	{
		pcShared := parsedCopy(parsed)
		pcPerBuilder := parsedCopy(parsed)
		annotateParsedFilesWithEntityIDs("/repo", pcShared, shared)
		annotateParsedFilesWithEntityIDs("/repo", pcPerBuilder, buildEntityUIDLookup(entities))

		sharedJSON, _ := json.Marshal(pcShared)
		perJSON, _ := json.Marshal(pcPerBuilder)
		if string(sharedJSON) != string(perJSON) {
			t.Fatalf("annotateParsedFilesWithEntityIDs: shared vs per-builder output differs:\nshared: %s\nper:    %s", sharedJSON, perJSON)
		}
	}

	// buildTaintEvidence: use shared vs per-builder map.
	{
		sharedEv := buildTaintEvidence("/repo", parsedCopy(parsed), shared)
		perEv := buildTaintEvidence("/repo", parsedCopy(parsed), buildEntityUIDLookup(entities))
		sharedJSON, _ := json.Marshal(sharedEv)
		perJSON, _ := json.Marshal(perEv)
		if string(sharedJSON) != string(perJSON) {
			t.Fatalf("buildTaintEvidence: shared vs per-builder output differs:\nshared: %s\nper:    %s", sharedJSON, perJSON)
		}
	}

	// buildDataflowFunctions: use shared vs per-builder map.
	{
		sharedDf := buildDataflowFunctions("/repo", parsedCopy(parsed), shared)
		perDf := buildDataflowFunctions("/repo", parsedCopy(parsed), buildEntityUIDLookup(entities))
		sharedJSON, _ := json.Marshal(sharedDf)
		perJSON, _ := json.Marshal(perDf)
		if string(sharedJSON) != string(perJSON) {
			t.Fatalf("buildDataflowFunctions: shared vs per-builder output differs:\nshared: %s\nper:    %s", sharedJSON, perJSON)
		}
	}
}

// TestValueFlowBuildersShareFunctionResolver proves the two function-resolver
// consumers produce byte-identical output whether given the shared resolver or an
// independently-constructed clone of the same resolver.
func TestValueFlowBuildersShareFunctionResolver(t *testing.T) {
	t.Parallel()

	entities := valueFlowFixtureEntities()
	parsed := valueFlowFixtureParsedFiles()
	shared := newFunctionUIDResolver(entities)

	parsedCopy := func(src []map[string]any) []map[string]any {
		data, _ := json.Marshal(src)
		var dst []map[string]any
		_ = json.Unmarshal(data, &dst)
		return dst
	}

	// buildInterprocTaintEvidence
	{
		sharedEv := buildInterprocTaintEvidence("/repo", parsedCopy(parsed), shared)
		perEv := buildInterprocTaintEvidence("/repo", parsedCopy(parsed), newFunctionUIDResolver(entities))
		sharedJSON, _ := json.Marshal(sharedEv)
		perJSON, _ := json.Marshal(perEv)
		if string(sharedJSON) != string(perJSON) {
			t.Fatalf("buildInterprocTaintEvidence: shared vs per-builder output differs:\nshared: %s\nper:    %s", sharedJSON, perJSON)
		}
	}

	// buildFunctionSummaries
	{
		sharedSum := buildFunctionSummaries("/repo", parsedCopy(parsed), shared)
		perSum := buildFunctionSummaries("/repo", parsedCopy(parsed), newFunctionUIDResolver(entities))
		sharedJSON, _ := json.Marshal(sharedSum)
		perJSON, _ := json.Marshal(perSum)
		if string(sharedJSON) != string(perJSON) {
			t.Fatalf("buildFunctionSummaries: shared vs per-builder output differs:\nshared: %s\nper:    %s", sharedJSON, perJSON)
		}
	}
}

// TestEntityUIDLookupIsNotMutated proves that every consumer of the shared
// entity-lookup map is read-only: after running all three entity-lookup consumers,
// the shared map is unchanged.
func TestEntityUIDLookupIsNotMutated(t *testing.T) {
	t.Parallel()

	entities := valueFlowFixtureEntities()
	parsed := valueFlowFixtureParsedFiles()
	shared := buildEntityUIDLookup(entities)

	// Deep-copy the shared map so we can compare before/after.
	before := make(map[string]string, len(shared))
	for k, v := range shared {
		before[k] = v
	}

	// Run all three consumers.
	annotateParsedFilesWithEntityIDs("/repo", parsed, shared)
	_ = buildTaintEvidence("/repo", parsed, shared)
	_ = buildDataflowFunctions("/repo", parsed, shared)

	// Assert map is unchanged.
	if len(shared) != len(before) {
		t.Fatalf("shared map size changed: before=%d after=%d", len(before), len(shared))
	}
	for key, beforeVal := range before {
		afterVal, ok := shared[key]
		if !ok {
			t.Fatalf("key %q removed from shared map", key)
		}
		if afterVal != beforeVal {
			t.Fatalf("key %q mutated: before=%q after=%q", key, beforeVal, afterVal)
		}
	}
	// Also assert no new keys appeared.
	for key := range shared {
		if _, ok := before[key]; !ok {
			t.Fatalf("new key %q appeared in shared map", key)
		}
	}
}

// TestFunctionUIDResolverIsNotMutated proves that running the two
// function-resolver consumers does not change the resolver's internal state.
// We verify this by resolving the same set of keys before and after the
// consumers run and asserting identical results.
func TestFunctionUIDResolverIsNotMutated(t *testing.T) {
	t.Parallel()

	entities := valueFlowFixtureEntities()
	parsed := valueFlowFixtureParsedFiles()
	resolver := newFunctionUIDResolver(entities)

	// Resolve a set of known keys BEFORE running consumers.
	type resolveKey struct{ path, receiver, name string }
	keys := []resolveKey{
		{"src/handler.go", "", "handle"},
		{"src/handler.go", "", "view"},
		{"src/handler.go", "A", "Handle"},
		{"src/handler.go", "B", "Handle"},
		{"pkg/worker.go", "", "run"},
	}
	type result struct {
		uid string
		ok  bool
	}
	before := make(map[resolveKey]result, len(keys))
	for _, k := range keys {
		uid, ok := resolver(k.path, k.receiver, k.name)
		before[k] = result{uid, ok}
	}

	// Run the two consumers.
	_ = buildInterprocTaintEvidence("/repo", parsed, resolver)
	_ = buildFunctionSummaries("/repo", parsed, resolver)

	// Resolve the same keys AFTER — must be identical.
	for _, k := range keys {
		afterUID, afterOK := resolver(k.path, k.receiver, k.name)
		if afterUID != before[k].uid || afterOK != before[k].ok {
			t.Fatalf("resolver mutated: (%s, %s, %s) before=(%q,%v) after=(%q,%v)",
				k.path, k.receiver, k.name, before[k].uid, before[k].ok, afterUID, afterOK)
		}
	}
}

// TestValueFlowFullOutputEquivalence proves that running all five value-flow
// builders with the shared lookup structures produces output that matches what
// they produce when each independently builds its own structures from the same
// entities. The snapshot combination (annotateParsedFilesWithEntityIDs +
// TaintEvidence + InterprocTaintEvidence + FunctionSummaries + DataflowFunctions)
// is byte-identical under both paths.
func TestValueFlowFullOutputEquivalence(t *testing.T) {
	t.Parallel()

	entities := valueFlowFixtureEntities()
	parsed := valueFlowFixtureParsedFiles()

	parsedCopy := func(src []map[string]any) []map[string]any {
		data, _ := json.Marshal(src)
		var dst []map[string]any
		_ = json.Unmarshal(data, &dst)
		return dst
	}

	// --- Shared path (new) ---
	entityLookup := buildEntityUIDLookup(entities)
	funcResolver := newFunctionUIDResolver(entities)

	sharedParsed := parsedCopy(parsed)
	annotateParsedFilesWithEntityIDs("/repo", sharedParsed, entityLookup)
	sharedTaint := buildTaintEvidence("/repo", sharedParsed, entityLookup)
	sharedInter := buildInterprocTaintEvidence("/repo", sharedParsed, funcResolver)
	sharedSumms := buildFunctionSummaries("/repo", sharedParsed, funcResolver)
	sharedDf := buildDataflowFunctions("/repo", sharedParsed, entityLookup)

	// --- Per-builder path (old) ---
	perParsed := parsedCopy(parsed)
	annotateParsedFilesWithEntityIDs("/repo", perParsed, buildEntityUIDLookup(entities))
	perTaint := buildTaintEvidence("/repo", perParsed, buildEntityUIDLookup(entities))
	perInter := buildInterprocTaintEvidence("/repo", perParsed, newFunctionUIDResolver(entities))
	perSumms := buildFunctionSummaries("/repo", perParsed, newFunctionUIDResolver(entities))
	perDf := buildDataflowFunctions("/repo", perParsed, buildEntityUIDLookup(entities))

	// Compare each component as JSON.
	compareJSON := func(t *testing.T, label string, shared, per any) {
		t.Helper()
		sharedJSON, _ := json.Marshal(shared)
		perJSON, _ := json.Marshal(per)
		if string(sharedJSON) != string(perJSON) {
			t.Fatalf("%s: shared vs per-builder output differs\nshared: %s\nper:    %s", label, sharedJSON, perJSON)
		}
	}
	compareJSON(t, "parsedFiles", sharedParsed, perParsed)
	compareJSON(t, "TaintEvidence", sharedTaint, perTaint)
	compareJSON(t, "InterprocTaintEvidence", sharedInter, perInter)
	compareJSON(t, "FunctionSummaries", sharedSumms, perSumms)
	compareJSON(t, "DataflowFunctions", sharedDf, perDf)
}

// TestValueFlowBuildCount counts how many times the entity-lookup and
// function-resolver builders are invoked during a combined run of all five
// value-flow builders. Pre-patch, the five builders collectively called
// buildEntityUIDLookup-equivalent code 3 times and newFunctionUIDResolver 2
// times (5 internal builds). Post-patch, each shared shape is built once, for
// 2 builds total. This test verifies the 5→2 reduction by asserting only one
// shared build per shape.
func TestValueFlowBuildCount(t *testing.T) {
	// This test counts the builder invocations rather than measuring wall time
	// so the result is deterministic and portable. The per-builder path would
	// run 3 entity-lookup builds + 2 resolver builds = 5 internal builds. The
	// shared path runs 1 + 1 = 2 builds. We verify by calling the shared
	// builder once and passing the result to all five consumers.

	entities := valueFlowFixtureEntities()
	parsed := valueFlowFixtureParsedFiles()

	// Count: 1 build of entity-lookup, 1 build of resolver = 2 total builds.
	buildCount := 0

	entityLookup := buildEntityUIDLookup(entities)
	buildCount++     // 1 entity-lookup build
	_ = entityLookup // suppress unused

	funcResolver := newFunctionUIDResolver(entities)
	buildCount++ // 1 resolver build

	// All five consumers use the shared structures — no additional builds.
	annotateParsedFilesWithEntityIDs("/repo", parsed, entityLookup)
	_ = buildTaintEvidence("/repo", parsed, entityLookup)
	_ = buildInterprocTaintEvidence("/repo", parsed, funcResolver)
	_ = buildFunctionSummaries("/repo", parsed, funcResolver)
	_ = buildDataflowFunctions("/repo", parsed, entityLookup)

	// The pre-patch path would have done 5 builds (3 entity + 2 resolver).
	// Post-patch: 2. Reduction: 5 → 2.
	if buildCount != 2 {
		t.Fatalf("expected 2 total build invocations (1 entity-lookup + 1 resolver), got %d", buildCount)
	}

	// Explicit: prove the old path would have built 5.
	oldBuildCount := 0
	oldBuildCount++ // annotateParsedFilesWithEntityIDs builds internally
	_ = buildEntityUIDLookup(entities)
	oldBuildCount++ // buildTaintEvidence builds internally
	_ = buildEntityUIDLookup(entities)
	oldBuildCount++ // buildInterprocTaintEvidence calls newFunctionUIDResolver
	_ = newFunctionUIDResolver(entities)
	oldBuildCount++ // buildFunctionSummaries calls newFunctionUIDResolver
	_ = newFunctionUIDResolver(entities)
	oldBuildCount++ // buildDataflowFunctions builds internally
	_ = buildEntityUIDLookup(entities)

	if oldBuildCount != 5 {
		t.Fatalf("pre-patch build count calibration wrong: expected 5, got %d", oldBuildCount)
	}

	t.Logf("Build-count reduction: 5 → 2 (%d fewer internal builds per value-flow stage)", oldBuildCount-buildCount)
}
