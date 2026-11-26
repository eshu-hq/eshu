// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractScopesSlice2FilterEcho pins the lane-C aggregate-scope fixes:
// every bare scope on the SBOM, security-alert, container-image,
// package-registry, and CI/CD aggregate routes must carry the filter-echo map
// shape (map[string]string of applied filters) with the console note to
// render chips, and each route's undocumented count/bucket siblings must
// carry descriptions derived from the Go owner that emits them.
func TestContractScopesSlice2FilterEcho(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec has no paths object")
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}

	schemaAt := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s 200 response has no content (only a description)", path, method)
		}
		body := child(t, content, "application/json", path+" "+method)
		return child(t, body, "schema", path+" "+method)
	}

	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
	}

	// requireFilterEchoScope asserts the slice-1 scope idiom: a string-valued
	// map echo of applied filters with the console chips note.
	requireFilterEchoScope := func(t *testing.T, props map[string]any, where string) {
		t.Helper()
		node, ok := props["scope"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing scope property", where)
		}
		if node["type"] != "object" {
			t.Errorf("%s: scope is not an object", where)
		}
		additional, ok := node["additionalProperties"].(map[string]any)
		if !ok || additional["type"] != "string" {
			t.Errorf("%s: scope has no string additionalProperties (not a filter-echo map)", where)
		}
		desc, _ := node["description"].(string)
		if !strings.Contains(desc, "string pairs") {
			t.Errorf("%s: scope description does not say string pairs: %q", where, desc)
		}
		if !strings.Contains(desc, "filter chips") {
			t.Errorf("%s: scope description has no console chips note: %q", where, desc)
		}
	}

	// requireDescribed asserts every named property exists and is described.
	requireDescribed := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
		}
	}

	// requireScalarTypes pins the JSON scalar type of hand-written schema
	// properties. Presence-only checks pass a boolean where the server
	// emits an integer, so every hand-typed scalar needs its type pinned.
	requireScalarTypes := func(t *testing.T, props map[string]any, where string, want map[string]string) {
		t.Helper()
		for key, typ := range want {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if got, _ := node["type"].(string); got != typ {
				t.Errorf("%s: property %q has type %q, want %q", where, key, got, typ)
			}
		}
	}

	// requireIntegerMaps asserts each named property is a string-keyed map of
	// integers (the bucket-tally shape): a boolean or free-form value here
	// would be a wire-type mismatch.
	requireIntegerMaps := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if node["type"] != "object" {
				t.Errorf("%s: property %q is not an object", where, key)
				continue
			}
			additional, ok := node["additionalProperties"].(map[string]any)
			if !ok || additional["type"] != "integer" {
				t.Errorf("%s: property %q is not an integer-valued map", where, key)
			}
		}
	}

	// requireInventoryPage asserts the shared inventory envelope: typed bucket
	// rows plus the described paging fields.
	requireInventoryPage := func(t *testing.T, path string) {
		t.Helper()
		where := path + " inventory"
		props := propsOf(t, schemaAt(t, path, "get"), where)
		buckets, ok := props["buckets"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing buckets property", where)
		}
		items, ok := buckets["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: buckets has no items", where)
		}
		bucketProps := propsOf(t, items, where+" buckets[]")
		requireDescribed(t, bucketProps, where+" buckets[]",
			"dimension", "value", "count")
		requireScalarTypes(t, bucketProps, where+" buckets[]", map[string]string{
			"dimension": "string", "value": "string", "count": "integer",
		})
		requireDescribed(t, props, where,
			"count", "limit", "offset", "group_by", "truncated", "next_offset")
		requireScalarTypes(t, props, where, map[string]string{
			"count": "integer", "limit": "integer", "offset": "integer",
			"group_by": "string", "truncated": "boolean",
		})
		requireFilterEchoScope(t, props, where)
	}

	t.Run("sbom attachment count", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/supply-chain/sbom-attestations/attachments/count", "get"), "sbom-count")
		requireDescribed(t, props, "sbom-count",
			"total_attachments", "by_attachment_status", "by_artifact_kind", "missing_evidence")
		requireScalarTypes(t, props, "sbom-count", map[string]string{"total_attachments": "integer"})
		requireIntegerMaps(t, props, "sbom-count", "by_attachment_status", "by_artifact_kind")
		requireFilterEchoScope(t, props, "sbom-count")
	})

	t.Run("sbom attachment inventory", func(t *testing.T) {
		t.Parallel()
		requireInventoryPage(t, "/api/v0/supply-chain/sbom-attestations/attachments/inventory")
	})

	t.Run("security alert count", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/supply-chain/security-alerts/reconciliations/count", "get"), "security-count")
		requireDescribed(t, props, "security-count",
			"total_reconciliations", "by_reconciliation_status", "by_provider",
			"by_provider_state", "by_source_freshness")
		requireScalarTypes(t, props, "security-count", map[string]string{"total_reconciliations": "integer"})
		requireIntegerMaps(t, props, "security-count", "by_reconciliation_status", "by_provider",
			"by_provider_state", "by_source_freshness")
		requireFilterEchoScope(t, props, "security-count")
	})

	t.Run("security alert inventory", func(t *testing.T) {
		t.Parallel()
		requireInventoryPage(t, "/api/v0/supply-chain/security-alerts/reconciliations/inventory")
	})

	t.Run("container image identity count", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/supply-chain/container-images/identities/count", "get"), "container-count")
		requireDescribed(t, props, "container-count",
			"total_identities", "by_outcome", "by_identity_strength")
		requireScalarTypes(t, props, "container-count", map[string]string{"total_identities": "integer"})
		requireIntegerMaps(t, props, "container-count", "by_outcome", "by_identity_strength")
		requireFilterEchoScope(t, props, "container-count")
	})

	t.Run("container image identity inventory", func(t *testing.T) {
		t.Parallel()
		requireInventoryPage(t, "/api/v0/supply-chain/container-images/identities/inventory")
	})

	t.Run("package registry count", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/package-registry/packages/count", "get"), "registry-count")
		requireDescribed(t, props, "registry-count", "total_packages", "by_ecosystem")
		requireScalarTypes(t, props, "registry-count", map[string]string{"total_packages": "integer"})
		requireIntegerMaps(t, props, "registry-count", "by_ecosystem")
		requireFilterEchoScope(t, props, "registry-count")
	})

	t.Run("package registry inventory", func(t *testing.T) {
		t.Parallel()
		requireInventoryPage(t, "/api/v0/package-registry/packages/inventory")
	})

	t.Run("cicd run correlation count", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/ci-cd/run-correlations/count", "get"), "cicd-count")
		requireDescribed(t, props, "cicd-count",
			"total_correlations", "by_outcome", "by_environment", "by_provider")
		requireScalarTypes(t, props, "cicd-count", map[string]string{"total_correlations": "integer"})
		requireIntegerMaps(t, props, "cicd-count", "by_outcome", "by_environment", "by_provider")
		requireFilterEchoScope(t, props, "cicd-count")
	})

	t.Run("cicd run correlation inventory", func(t *testing.T) {
		t.Parallel()
		requireInventoryPage(t, "/api/v0/ci-cd/run-correlations/inventory")
	})
}
