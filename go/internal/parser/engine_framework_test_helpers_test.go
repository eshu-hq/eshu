// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"reflect"
	"testing"
)

// assertNoFrameworkOrNoRoutes used to live here, but its only caller
// (engine_javascript_koa_fastify_nestjs_route_entries_test.go and
// engine_javascript_koa_router_require_route_entries_test.go) relocated to
// internal/parser/javascript as part of #6062. The relocated javascript_test
// package keeps its own copy in engine_javascript_test_helpers_test.go.

// frameworkSemanticsMap, nestedSemanticsSection, assertFrameworksEqual,
// assertNestedStringSliceEqual, assertNestedRouteEntriesEqual,
// findNamedBucketItem, and findAllNamedBucketItems used to live in
// engine_javascript_semantics_test.go. That file relocated to
// internal/parser/javascript as part of #6062, but Python, Java, C#, PHP, Go,
// TypeScript, and TSX engine tests across this package still call them, so
// this shared, uncontended helper file keeps its own copy for the parent
// package. The relocated javascript_test package keeps an independent copy in
// engine_javascript_test_helpers_test.go.

func frameworkSemanticsMap(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics = %T, want map[string]any", payload["framework_semantics"])
	}
	return semantics
}

func nestedSemanticsSection(t *testing.T, payload map[string]any, section string) map[string]any {
	t.Helper()

	semantics := frameworkSemanticsMap(t, payload)
	nested, ok := semantics[section].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics.%s = %T, want map[string]any", section, semantics[section])
	}
	return nested
}

func assertFrameworksEqual(t *testing.T, payload map[string]any, want ...string) {
	t.Helper()

	semantics := frameworkSemanticsMap(t, payload)
	got, ok := semantics["frameworks"].([]string)
	if !ok {
		t.Fatalf("framework_semantics.frameworks = %T, want []string", semantics["frameworks"])
	}
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frameworks = %#v, want %#v", got, want)
	}
}

func assertNestedStringSliceEqual(
	t *testing.T,
	payload map[string]any,
	section string,
	key string,
	want []string,
) {
	t.Helper()

	nested := nestedSemanticsSection(t, payload, section)
	got, ok := nested[key].([]string)
	if !ok {
		t.Fatalf("framework_semantics.%s.%s = %T, want []string", section, key, nested[key])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("framework_semantics.%s.%s = %#v, want %#v", section, key, got, want)
	}
}

func assertNestedRouteEntriesEqual(
	t *testing.T,
	payload map[string]any,
	section string,
	want []map[string]string,
) {
	t.Helper()

	nested := nestedSemanticsSection(t, payload, section)
	raw, ok := nested["route_entries"].([]map[string]string)
	if !ok {
		t.Fatalf("framework_semantics.%s.route_entries = %T, want []map[string]string", section, nested["route_entries"])
	}
	if !reflect.DeepEqual(raw, want) {
		t.Fatalf("framework_semantics.%s.route_entries = %#v, want %#v", section, raw, want)
	}
}

func findNamedBucketItem(t *testing.T, payload map[string]any, key string, name string) map[string]any {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		itemName, _ := item["name"].(string)
		if itemName == name {
			return item
		}
	}
	t.Fatalf("%s missing item with name %q", key, name)
	return nil
}

func findAllNamedBucketItems(t *testing.T, payload map[string]any, key string, name string) []map[string]any {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	matches := make([]map[string]any, 0)
	for _, item := range items {
		itemName, _ := item["name"].(string)
		if itemName == name {
			matches = append(matches, item)
		}
	}
	return matches
}
