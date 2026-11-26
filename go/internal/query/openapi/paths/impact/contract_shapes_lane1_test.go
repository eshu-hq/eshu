// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLane1Impact pins the lane-1 shape-completeness fixes for
// the impact path family: every 200-response object the console renders must
// name its properties in the fragment instead of a bare {"type": "object"},
// every 200-response property must carry a description naming its emitting
// owner, and string-map scopes must use the filter-echo idiom. Each entry
// navigates the fragment JSON and requires the property keys the Go owner
// actually emits; invented shapes fail review.
func TestContractShapesLane1Impact(t *testing.T) {
	t.Parallel()

	fragment := func(t *testing.T, raw, where string) map[string]any {
		t.Helper()
		trimmed := strings.TrimSpace(raw)
		trimmed = strings.TrimSuffix(trimmed, ",")
		var doc map[string]any
		if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
			t.Fatalf("%s: fragment is not valid JSON: %v", where, err)
		}
		return doc
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}

	schemaAt := func(t *testing.T, doc map[string]any, path, method string) map[string]any {
		t.Helper()
		route, ok := doc[path].(map[string]any)
		if !ok {
			t.Fatalf("fragment paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s 200 response has no content", path, method)
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

	itemsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: array schema has no items", where)
		}
		return items
	}

	requireKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
	}

	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		want := make(map[string]bool, len(keys))
		for _, key := range keys {
			want[key] = true
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
		for key := range props {
			if !want[key] {
				t.Errorf("%s: phantom property %q not emitted by the owner", where, key)
			}
		}
	}

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

	// requireNoBareObjects fails on any {"type": "object"} without
	// properties or additionalProperties, and any array items object
	// without properties or additionalProperties. An object with an
	// explicitly empty properties map documents an always-empty shape
	// (e.g. consumers, which the owner never populates); it passes.
	var requireNoBareObjects func(t *testing.T, node any, where string)
	requireNoBareObjects = func(t *testing.T, node any, where string) {
		t.Helper()
		obj, ok := node.(map[string]any)
		if !ok {
			return
		}
		if obj["type"] == "object" {
			_, hasProps := obj["properties"]
			_, hasAdditional := obj["additionalProperties"]
			if !hasProps && !hasAdditional {
				t.Errorf("%s: bare object with no properties", where)
			}
			if props, ok := obj["properties"].(map[string]any); ok {
				for key, sub := range props {
					requireNoBareObjects(t, sub, where+"."+key)
				}
			}
			if addl, ok := obj["additionalProperties"].(map[string]any); ok {
				requireNoBareObjects(t, addl, where+"[]")
			}
		}
		if obj["type"] == "array" {
			items, ok := obj["items"].(map[string]any)
			if !ok {
				t.Errorf("%s: array schema has no items", where)
				return
			}
			if items["type"] == "object" {
				_, hasProps := items["properties"]
				_, hasAdditional := items["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: array has bare object items", where)
				}
			}
			requireNoBareObjects(t, items, where+"[]")
		}
		for key, sub := range obj {
			if key == "properties" || key == "items" || key == "additionalProperties" {
				continue
			}
			requireNoBareObjects(t, sub, where+"."+key)
		}
	}

	t.Run("contracts route", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/contract.go contractImpactResponse,
		// contractImpactScope, contractImpactFamilyStates,
		// contractImpactHTTPProviders.
		props := propsOf(t, schemaAt(t, fragment(t, Contract, "Contract"), "/api/v0/impact/contracts", "post"), "contracts")
		requireDescribed(t, props, "contracts",
			"family", "scope", "families", "providers", "consumers", "coverage", "limit", "truncated")
		requireScalarTypes(t, props, "contracts", map[string]string{
			"family": "string", "providers": "array", "consumers": "array",
			"limit": "integer", "truncated": "boolean",
		})
		requireFilterEchoScope(t, props, "contracts")
		families := child(t, props, "families", "contracts")
		requireExactKeys(t, propsOf(t, families, "contracts.families"), "contracts.families",
			"http", "topic", "grpc", "selected")
		for _, fam := range []string{"http", "topic", "grpc"} {
			famProps := propsOf(t, child(t, propsOf(t, families, "contracts.families"), fam, "contracts.families"), "contracts.families."+fam)
			requireExactKeys(t, famProps, "contracts.families."+fam, "state", "reason")
		}
		providers := child(t, props, "providers", "contracts")
		providerProps := propsOf(t, itemsOf(t, providers, "contracts providers"), "contracts providers[]")
		requireKeys(t, providerProps, "contracts providers[]",
			"family", "contract_key", "provider_repo_id", "provider_repo", "route",
			"http_method", "endpoint_id", "source_kinds", "source_paths",
			"operation_ids", "evidence_state", "workload_id", "workload_name")
		coverage := child(t, props, "coverage", "contracts")
		coverageProps := propsOf(t, coverage, "contracts.coverage")
		requireExactKeys(t, coverageProps, "contracts.coverage",
			"query_shape", "deterministic_only", "string_similarity_edges")
		requireScalarTypes(t, coverageProps, "contracts.coverage", map[string]string{
			"query_shape": "string", "deterministic_only": "boolean", "string_similarity_edges": "boolean",
		})
	})

	t.Run("blast radius", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/blast_radius.go findBlastRadius; affected rows
		// always carry repo, with tier/risk/hops/repo_id/claim only when
		// the enrichment or traversal produced them.
		doc := fragment(t, Routes, "Routes")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/blast-radius", "post"), "blast-radius")
		requireDescribed(t, props, "blast-radius",
			"target", "target_type", "affected", "affected_count", "limit", "truncated", "complete", "coverage")
		requireScalarTypes(t, props, "blast-radius", map[string]string{
			"target": "string", "target_type": "string", "affected": "array",
			"affected_count": "integer", "limit": "integer", "truncated": "boolean", "complete": "boolean",
		})
		affected := child(t, props, "affected", "blast-radius")
		affectedProps := propsOf(t, itemsOf(t, affected, "blast-radius affected"), "blast-radius affected[]")
		requireKeys(t, affectedProps, "blast-radius affected[]",
			"repo", "tier", "risk", "hops", "repo_id", "claim")
		requireScalarTypes(t, affectedProps, "blast-radius affected[]", map[string]string{
			"repo": "string", "tier": "string", "risk": "string",
			"hops": "integer", "repo_id": "string", "claim": "string",
		})
	})

	t.Run("change surface legacy", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/change_surface_legacy.go findChangeSurface and
		// FindChangeSurfaceImpactRows: target is {id} unresolved or
		// {id, name} resolved; impacted rows carry per-edge provenance
		// with rel_type/confidence/reason only when the edge has them.
		doc := fragment(t, Routes, "Routes")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/change-surface", "post"), "change-surface")
		requireDescribed(t, props, "change-surface",
			"target", "impacted", "count", "limit", "truncated")
		targetProps := propsOf(t, child(t, props, "target", "change-surface"), "change-surface.target")
		requireKeys(t, targetProps, "change-surface.target", "id", "name")
		impacted := child(t, props, "impacted", "change-surface")
		impactedProps := propsOf(t, itemsOf(t, impacted, "change-surface impacted"), "change-surface impacted[]")
		requireKeys(t, impactedProps, "change-surface impacted[]",
			"id", "name", "labels", "depth", "environment", "rel_type", "confidence", "reason")
		requireScalarTypes(t, impactedProps, "change-surface impacted[]", map[string]string{
			"id": "string", "name": "string", "labels": "array", "depth": "integer",
			"environment": "string", "rel_type": "string", "confidence": "number", "reason": "string",
		})
	})

	t.Run("change surface investigate", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/change_surface_response.go changeSurfaceResponse,
		// changeSurfaceScope, changeSurfaceImpactEntry; resolution from
		// resolveChangeSurfaceTarget; code_surface from
		// family_impact_change_surface_code.go FetchCodeSurface.
		doc := fragment(t, Routes, "Routes")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/change-surface/investigate", "post"), "investigate")
		requireDescribed(t, props, "investigate",
			"scope", "target_resolution", "code_surface", "direct_impact",
			"transitive_impact", "impact_summary", "recommended_next_calls",
			"coverage", "limit", "offset", "truncated", "source_backend", "answer_metadata")
		scopeProps := propsOf(t, child(t, props, "scope", "investigate"), "investigate.scope")
		requireExactKeys(t, scopeProps, "investigate.scope",
			"repo_id", "environment", "target", "target_type", "changed_paths",
			"topic", "limit", "offset", "max_depth")
		impact := child(t, props, "direct_impact", "investigate")
		impactProps := propsOf(t, itemsOf(t, impact, "investigate direct"), "investigate direct_impact[]")
		requireKeys(t, impactProps, "investigate direct_impact[]",
			"id", "name", "labels", "depth", "evidence_handle", "environment", "repo_id")
		handleProps := propsOf(t, child(t, impactProps, "evidence_handle", "impact row"), "impact evidence_handle")
		requireExactKeys(t, handleProps, "impact evidence_handle", "entity_id")
		summaryProps := propsOf(t, child(t, props, "impact_summary", "investigate"), "investigate.impact_summary")
		requireExactKeys(t, summaryProps, "investigate.impact_summary",
			"direct_count", "transitive_count", "total_count")
	})

	t.Run("pre-change", func(t *testing.T) {
		t.Parallel()
		// Owner: impact/prechange.go preChangeImpactData,
		// preChangeMissingEvidence, preChangeCoverage, preChangeFileMaps.
		doc := fragment(t, Rest, "Rest")
		props := propsOf(t, schemaAt(t, doc, "/api/v0/impact/pre-change", "post"), "pre-change")
		requireDescribed(t, props, "pre-change",
			"workflow", "change_set", "changed_files", "changed_file_count",
			"code_surface", "direct_impact", "transitive_impact", "impact_summary",
			"missing_evidence", "coverage", "recommended_next_calls",
			"truncated", "answer_metadata", "answer_packet")
		changeSetProps := propsOf(t, child(t, props, "change_set", "pre-change"), "pre-change.change_set")
		requireExactKeys(t, changeSetProps, "pre-change.change_set",
			"repo_id", "base_ref", "head_ref", "mode")
		files := child(t, props, "changed_files", "pre-change")
		fileProps := propsOf(t, itemsOf(t, files, "pre-change files"), "pre-change changed_files[]")
		requireKeys(t, fileProps, "pre-change changed_files[]",
			"repo_id", "path", "status", "source_handle", "old_path")
		missing := child(t, props, "missing_evidence", "pre-change")
		missingProps := propsOf(t, itemsOf(t, missing, "pre-change missing"), "pre-change missing_evidence[]")
		requireExactKeys(t, missingProps, "pre-change missing_evidence[]",
			"kind", "repo_id", "relative_path", "status", "reason")
	})
}
