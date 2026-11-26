// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneHFinding pins the Lane H gap-1 fix: the `finding`
// node on POST /api/v0/iac/management-status and POST
// /api/v0/iac/management-status/explain spells out the
// iac.ManagementFindingRow wire shape instead of a bare list-typed
// ["object","null"] with no properties. Both handlers emit the same
// *ManagementFindingRow (nil when no active finding), so both nodes share
// one required set and one conditional-presence convention.
func TestContractShapesLaneHFinding(t *testing.T) {
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

	findingAt := func(t *testing.T, raw, path, where string) map[string]any {
		t.Helper()
		doc := fragment(t, raw, where)
		route, ok := doc[path].(map[string]any)
		if !ok {
			t.Fatalf("%s: fragment paths missing %s", where, path)
		}
		post, ok := route["post"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s missing post", where, path)
		}
		responses, ok := post["responses"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s post missing responses", where, path)
		}
		okResp, ok := responses["200"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s post missing 200", where, path)
		}
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 missing content", where, path)
		}
		body, ok := content["application/json"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 missing application/json", where, path)
		}
		schema, ok := body["schema"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 missing schema", where, path)
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 schema missing properties", where, path)
		}
		finding, ok := props["finding"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 schema missing finding", where, path)
		}
		return finding
	}

	// requiredKeys are the ManagementFindingRow fields without `omitempty`:
	// always present when finding is non-null.
	requiredKeys := []string{
		"id", "provider", "finding_kind", "management_status",
		"confidence", "scope_id", "generation_id", "source_system",
		"recommended_action", "safety_gate", "evidence",
	}
	// conditionalKeys are the `omitempty` fields: present only when the
	// row carries that signal.
	conditionalKeys := []string{
		"account_id", "region", "resource_type", "resource_id", "arn",
		"tags", "candidate_id", "matched_terraform_state_address",
		"matched_terraform_config_file", "matched_terraform_module_path",
		"matched_other_iac_source", "service_candidates",
		"environment_candidates", "dependency_paths", "missing_evidence",
		"warning_flags", "drifted_attributes",
	}

	for _, tc := range []struct {
		name string
		raw  string
		path string
	}{
		{"management-status", ManagementStatus, "/api/v0/iac/management-status"},
		{"explain", ManagementExplanation, "/api/v0/iac/management-status/explain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			finding := findingAt(t, tc.raw, tc.path, tc.name)

			types, ok := finding["type"].([]any)
			if !ok {
				t.Fatalf("%s: finding has no list type (want [\"object\",\"null\"])", tc.name)
			}
			seen := map[string]bool{}
			for _, typ := range types {
				if s, ok := typ.(string); ok {
					seen[s] = true
				}
			}
			if !seen["object"] || !seen["null"] {
				t.Fatalf("%s: finding type = %v, want [\"object\",\"null\"]", tc.name, types)
			}
			if desc, _ := finding["description"].(string); desc == "" {
				t.Fatalf("%s: finding has no description", tc.name)
			}

			props, ok := finding["properties"].(map[string]any)
			if !ok || len(props) == 0 {
				t.Fatalf("%s: finding is a bare list-typed object with no properties", tc.name)
			}
			for _, key := range requiredKeys {
				node, ok := props[key].(map[string]any)
				if !ok {
					t.Errorf("%s: finding missing required property %q", tc.name, key)
					continue
				}
				if desc, _ := node["description"].(string); !strings.Contains(desc, "iac.ManagementFindingRow") {
					t.Errorf("%s: finding.%s description does not name its iac.ManagementFindingRow owner", tc.name, key)
				}
			}
			for _, key := range conditionalKeys {
				node, ok := props[key].(map[string]any)
				if !ok {
					t.Errorf("%s: finding missing conditional property %q", tc.name, key)
					continue
				}
				desc, _ := node["description"].(string)
				if !strings.Contains(desc, "iac.ManagementFindingRow") {
					t.Errorf("%s: finding.%s description does not name its iac.ManagementFindingRow owner", tc.name, key)
				}
				if !strings.Contains(desc, "absent") {
					t.Errorf("%s: finding.%s description carries no presence note (want \"absent when ...\")", tc.name, key)
				}
			}
			if req, ok := finding["required"].([]any); ok {
				required := map[string]bool{}
				for _, r := range req {
					if s, ok := r.(string); ok {
						required[s] = true
					}
				}
				for _, key := range requiredKeys {
					if !required[key] {
						t.Errorf("%s: finding required omits %q", tc.name, key)
					}
				}
			} else {
				t.Errorf("%s: finding has no required list", tc.name)
			}

			safety, ok := props["safety_gate"].(map[string]any)
			if !ok {
				t.Fatalf("%s: finding missing safety_gate object", tc.name)
			}
			safetyProps, ok := safety["properties"].(map[string]any)
			if !ok || len(safetyProps) == 0 {
				t.Fatalf("%s: finding.safety_gate is a bare object", tc.name)
			}
			for _, key := range []string{"outcome", "read_only", "review_required", "audit_expectation"} {
				node, ok := safetyProps[key].(map[string]any)
				if !ok {
					t.Errorf("%s: finding.safety_gate missing %q", tc.name, key)
					continue
				}
				if desc, _ := node["description"].(string); !strings.Contains(desc, "packet.ManagementSafetyGate") {
					t.Errorf("%s: finding.safety_gate.%s description does not name its packet.ManagementSafetyGate owner", tc.name, key)
				}
			}

			evidence, ok := props["evidence"].(map[string]any)
			if !ok {
				t.Fatalf("%s: finding missing evidence array", tc.name)
			}
			items, ok := evidence["items"].(map[string]any)
			if !ok {
				t.Fatalf("%s: finding.evidence has no items", tc.name)
			}
			if desc, _ := items["description"].(string); !strings.Contains(desc, "iac.ManagementEvidenceRow") {
				t.Errorf("%s: finding.evidence items description does not name its iac.ManagementEvidenceRow owner", tc.name)
			}
			evidenceProps, ok := items["properties"].(map[string]any)
			if !ok || len(evidenceProps) == 0 {
				t.Fatalf("%s: finding.evidence items is a bare object", tc.name)
			}
			for _, key := range []string{"id", "source_system", "evidence_type", "scope_id", "key", "value", "confidence", "provenance_only"} {
				if _, ok := evidenceProps[key].(map[string]any); !ok {
					t.Errorf("%s: finding.evidence items missing %q", tc.name, key)
				}
			}

			drifted, ok := props["drifted_attributes"].(map[string]any)
			if !ok {
				t.Fatalf("%s: finding missing drifted_attributes array", tc.name)
			}
			driftedItems, ok := drifted["items"].(map[string]any)
			if !ok {
				t.Fatalf("%s: finding.drifted_attributes has no items", tc.name)
			}
			if desc, _ := driftedItems["description"].(string); !strings.Contains(desc, "querycontract.DriftedAttributeView") {
				t.Errorf("%s: finding.drifted_attributes items description does not name its querycontract.DriftedAttributeView owner", tc.name)
			}
		})
	}
}
