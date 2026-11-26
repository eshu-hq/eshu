// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneC14Chain pins the lane-C14 verification for the
// supply-chain leaf leftovers: impact_findings, runtime_context, and
// security_alert_aggregate carry no bare objects or undescribed 200-response
// properties. The test walks each fragment recursively so a future bare
// shape fails here instead of reaching the census.
func TestContractShapesLaneC14Chain(t *testing.T) {
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
	// requireNoBare fails on any object schema without properties,
	// additionalProperties, $ref, or composition, and on any array schema
	// without items. Empty properties ("properties": {}) count as bare.
	requireNoBare := func(t *testing.T, node any, where string) {
		t.Helper()
		var walk func(node any, path string)
		walk = func(node any, path string) {
			obj, ok := node.(map[string]any)
			if !ok {
				return
			}
			if ref, _ := obj["$ref"].(string); ref != "" {
				return
			}
			for _, comp := range []string{"oneOf", "anyOf", "allOf"} {
				if branches, ok := obj[comp].([]any); ok {
					for _, branch := range branches {
						walk(branch, path)
					}
					return
				}
			}
			if typ, _ := obj["type"].(string); typ == "object" || (typ == "" && obj["properties"] != nil) {
				props, hasProps := obj["properties"].(map[string]any)
				_, hasAdditional := obj["additionalProperties"]
				if (!hasProps || len(props) == 0) && !hasAdditional {
					t.Errorf("%s: bare object at %q (no properties/additionalProperties)", where, path)
					return
				}
				for key, sub := range props {
					walk(sub, path+"."+key)
				}
				if addl, ok := obj["additionalProperties"].(map[string]any); ok {
					walk(addl, path+"[]")
				}
				return
			}
			if typ, _ := obj["type"].(string); typ == "array" {
				items, ok := obj["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: array without items at %q", where, path)
					return
				}
				walk(items, path+"[]")
				return
			}
			for _, key := range []string{"properties", "items"} {
				if sub, ok := obj[key]; ok {
					walk(sub, path)
				}
			}
		}
		walk(node, where)
	}
	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}
	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
	}

	t.Run("impact findings has no bare shapes", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, impactFindings, "impact findings")
		requireNoBare(t, doc, "impact findings")
		// Owner: the runtime_context splice keeps list and explain
		// identical; truth_basis stays the required read-time label.
		route, ok := doc["/api/v0/supply-chain/impact/findings"].(map[string]any)
		if !ok {
			t.Fatal("fragment paths missing /api/v0/supply-chain/impact/findings")
		}
		op := child(t, route, "get", "impact findings")
		responses := child(t, op, "responses", "impact findings get")
		okResp := child(t, responses, "200", "impact findings get")
		content := child(t, okResp, "content", "impact findings get 200")
		body := child(t, content, "application/json", "impact findings get 200")
		schema := child(t, body, "schema", "impact findings get 200")
		props := propsOf(t, schema, "impact findings")
		findings := child(t, props, "findings", "impact findings")
		items, ok := findings["items"].(map[string]any)
		if !ok {
			t.Fatal("impact findings.findings: array has no items")
		}
		itemProps := propsOf(t, items, "impact findings.findings[]")
		if _, ok := itemProps["runtime_context"]; !ok {
			t.Error("impact findings.findings[]: missing runtime_context splice")
		}
		runtime := child(t, itemProps, "runtime_context", "impact findings.findings[]")
		if required, ok := runtime["required"].([]any); !ok || len(required) == 0 {
			t.Error("impact findings.findings[].runtime_context: must pin required keys")
		} else {
			found := false
			for _, key := range required {
				if key == "truth_basis" {
					found = true
				}
			}
			if !found {
				t.Error("impact findings.findings[].runtime_context: required must include truth_basis")
			}
		}
	})

	t.Run("security alert aggregates have no bare shapes", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, SecurityAlertAggregate, "security alert aggregate")
		requireNoBare(t, doc, "security alert aggregate")
		// Owner: the security-alert coverage classifier emits state,
		// partial_rows, rows_considered, and reasons; the reasons enum
		// must equal the handler closed set (TestSecurityAlertCoverageOpenAPIEnum).
		route, ok := doc["/api/v0/supply-chain/security-alerts/reconciliations/count"].(map[string]any)
		if !ok {
			t.Fatal("fragment paths missing reconciliations/count")
		}
		op := child(t, route, "get", "reconciliations count")
		responses := child(t, op, "responses", "reconciliations count get")
		okResp := child(t, responses, "200", "reconciliations count get")
		content := child(t, okResp, "content", "reconciliations count get 200")
		body := child(t, content, "application/json", "reconciliations count get 200")
		schema := child(t, body, "schema", "reconciliations count get 200")
		props := propsOf(t, schema, "reconciliations count")
		coverage, ok := props["coverage"].(map[string]any)
		if !ok {
			t.Fatal("reconciliations count: missing coverage")
		}
		coverageProps := propsOf(t, coverage, "reconciliations count.coverage")
		for _, key := range []string{"state", "partial_rows", "rows_considered", "reasons"} {
			if _, ok := coverageProps[key].(map[string]any); !ok {
				t.Errorf("reconciliations count.coverage: missing property %q", key)
			}
		}
		// Owner: documentationFindingAggregateScope-style filter echo:
		// the scope carries only non-empty filters as string pairs.
		scope, ok := props["scope"].(map[string]any)
		if !ok {
			t.Fatal("reconciliations count: missing scope")
		}
		if addl, ok := scope["additionalProperties"].(map[string]any); !ok || addl["type"] != "string" {
			t.Error("reconciliations count.scope: must be an open string map")
		}
	})
}
