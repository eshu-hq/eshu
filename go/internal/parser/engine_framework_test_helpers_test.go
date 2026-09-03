// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"testing"
)

// assertNoFrameworkOrNoRoutes used to live here, but its only caller
// (engine_javascript_koa_fastify_nestjs_route_entries_test.go and
// engine_javascript_koa_router_require_route_entries_test.go) relocated to
// internal/parser/javascript as part of #6062. The relocated javascript_test
// package keeps its own copy in engine_javascript_test_helpers_test.go.

// frameworkSemanticsMap, nestedSemanticsSection, assertFrameworksEqual,
// assertNestedStringSliceEqual, and assertNestedRouteEntriesEqual also used
// to live here (they arrived from engine_javascript_semantics_test.go when
// that file relocated to internal/parser/javascript). Their last parent-side
// caller, kotlin_spring_route_semantics_test.go, relocated to
// internal/parser/kotlin as part of #6062, and the relocated kotlin_test
// package uses the parsertest copies (AssertFrameworksEqual,
// AssertNestedStringSliceEqual, AssertNestedRouteEntriesEqual) instead.

// findAllNamedBucketItems used to live here too. Its last parent-side caller,
// engine_typescript_advanced_semantics_test.go, relocated to
// internal/parser/javascript as part of #6062, and the relocated
// javascript_test package uses the copy in engine_javascript_semantics_test.go.
// This file now keeps only findNamedBucketItem, whose remaining caller is
// engine_infra_test.go.

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
