// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB9IaC pins the lane-B9 shape-completeness fixes for
// the IaC dead-IaC, unmanaged-resource, import-plan, and management-status
// reads. Bare {"type": "object"} shapes gain properties (or a described
// open-map shape), and every 200-response property carries a description
// naming its Go owner.
func TestContractShapesLaneB9IaC(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
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
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if node["type"] == "object" {
				_, hasProps := node["properties"]
				_, hasAdditional := node["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: property %q is a bare object with no properties", where, key)
				}
			}
			if node["type"] == "array" {
				items, ok := node["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: property %q is an array with no items", where, key)
					continue
				}
				if items["type"] == "object" {
					_, hasProps := items["properties"]
					_, hasAdditional := items["additionalProperties"]
					if !hasProps && !hasAdditional {
						t.Errorf("%s: property %q has bare object items", where, key)
					}
				}
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

	schema200 := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		paths, ok := spec["paths"].(map[string]any)
		if !ok {
			t.Fatal("spec has no paths object")
		}
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content := child(t, okResp, "content", path+" "+method+" 200")
		body := child(t, content, "application/json", path+" "+method+" 200")
		return child(t, body, "schema", path+" "+method+" 200")
	}

	safetyGateKeys := []string{"outcome", "read_only", "review_required", "refused_actions", "warnings", "redactions", "audit_expectation"}
	driftedAttrKeys := []string{"attribute", "declared_value", "observed_value"}
	mgmtFindingKeys := []string{
		"id", "provider", "account_id", "region", "resource_type", "resource_id",
		"arn", "tags", "finding_kind", "management_status", "confidence",
		"scope_id", "generation_id", "source_system", "candidate_id",
		"matched_terraform_state_address", "matched_terraform_config_file",
		"matched_terraform_module_path", "matched_other_iac_source",
		"service_candidates", "environment_candidates", "dependency_paths",
		"recommended_action", "missing_evidence", "warning_flags",
		"safety_gate", "evidence", "drifted_attributes",
	}

	t.Run("dead iac findings", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/iac/dead"
		top := propsOf(t, schema200(t, "/api/v0/iac/dead", "post"), where)
		requireExactKeys(t, top, where,
			"repo_ids", "findings_count", "total_findings_count", "limit", "offset",
			"truncated", "next_offset", "truth_basis", "analysis_status",
			"limitations", "findings")
		requireDescribed(t, top, where,
			"repo_ids", "findings_count", "total_findings_count", "limit", "offset",
			"truncated", "next_offset", "truth_basis", "analysis_status",
			"limitations", "findings")
		items := itemsOf(t, top["findings"].(map[string]any), where+" findings")
		fprops := propsOf(t, items, where+" findings[]")
		// Owner: iac.deadIaCFinding via materializedDeadIaCFindings/analyzeDeadIaC.
		requireExactKeys(t, fprops, where+" findings[]",
			"id", "family", "repo_id", "repo_name", "artifact", "reachability",
			"finding", "confidence", "evidence", "limitations")
		requireDescribed(t, fprops, where+" findings[]",
			"id", "family", "repo_id", "repo_name", "artifact", "reachability",
			"finding", "confidence", "evidence", "limitations")
		requireScalarTypes(t, fprops, where+" findings[]", map[string]string{
			"id": "string", "confidence": "number",
		})
	})

	t.Run("unmanaged resources", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/iac/unmanaged-resources"
		top := propsOf(t, schema200(t, "/api/v0/iac/unmanaged-resources", "post"), where)
		requireExactKeys(t, top, where,
			"scope_id", "account_id", "region", "arn", "story", "finding_groups",
			"safety_summary", "finding_kinds", "findings_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis", "analysis_status",
			"graph_projection_note", "limitations", "findings")
		requireDescribed(t, top, where,
			"scope_id", "account_id", "region", "arn", "story", "finding_groups",
			"safety_summary", "finding_kinds", "findings_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis", "analysis_status",
			"graph_projection_note", "limitations", "findings")
		groups := itemsOf(t, top["finding_groups"].(map[string]any), where+" finding_groups")
		gprops := propsOf(t, groups, where+" finding_groups[]")
		// Owner: iac.managementFindingGroup via managementFindingGroups.
		requireExactKeys(t, gprops, where+" finding_groups[]",
			"management_status", "finding_kind", "count", "arns")
		requireDescribed(t, gprops, where+" finding_groups[]",
			"management_status", "finding_kind", "count", "arns")
		summary := propsOf(t, top["safety_summary"].(map[string]any), where+" safety_summary")
		// Owner: iac.managementSafetySummaryRow via managementSafetySummary.
		requireExactKeys(t, summary, where+" safety_summary",
			"total_findings", "review_required_count", "redacted_findings_count", "refused_actions")
		requireDescribed(t, summary, where+" safety_summary",
			"total_findings", "review_required_count", "redacted_findings_count", "refused_actions")
		items := itemsOf(t, top["findings"].(map[string]any), where+" findings")
		fprops := propsOf(t, items, where+" findings[]")
		// Owner: iac.ManagementFindingRow via management_transform.
		requireExactKeys(t, fprops, where+" findings[]", mgmtFindingKeys...)
		requireDescribed(t, fprops, where+" findings[]", mgmtFindingKeys...)
		gate := propsOf(t, fprops["safety_gate"].(map[string]any), where+" safety_gate")
		// Owner: packet.ManagementSafetyGate via normalizeIaCManagementFindingsSafety.
		requireExactKeys(t, gate, where+" safety_gate", safetyGateKeys...)
		requireDescribed(t, gate, where+" safety_gate", safetyGateKeys...)
		ev := itemsOf(t, fprops["evidence"].(map[string]any), where+" evidence")
		eprops := propsOf(t, ev, where+" evidence[]")
		// Owner: iac.ManagementEvidenceRow via sanitizeIaCManagementEvidence.
		requireExactKeys(t, eprops, where+" evidence[]",
			"id", "source_system", "evidence_type", "scope_id", "key", "value",
			"confidence", "provenance_only")
		requireDescribed(t, eprops, where+" evidence[]",
			"id", "source_system", "evidence_type", "scope_id", "key", "value",
			"confidence", "provenance_only")
		// Filter-map check: tags is a genuine open string map (tag key -> tag
		// value) emitted by iac.ManagementFindingRow.Tags.
		tags := fprops["tags"].(map[string]any)
		if _, ok := tags["additionalProperties"]; !ok {
			t.Errorf("%s: tags must carry additionalProperties for the open tag map", where)
		}
		requireDescribed(t, fprops, where+" findings[]", "tags")
		da := itemsOf(t, fprops["drifted_attributes"].(map[string]any), where+" drifted_attributes")
		daprops := propsOf(t, da, where+" drifted_attributes[]")
		requireExactKeys(t, daprops, where+" drifted_attributes[]", driftedAttrKeys...)
		requireDescribed(t, daprops, where+" drifted_attributes[]", driftedAttrKeys...)
	})

	t.Run("terraform import-plan candidates", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/iac/terraform-import-plan/candidates"
		top := propsOf(t, schema200(t, "/api/v0/iac/terraform-import-plan/candidates", "post"), where)
		requireExactKeys(t, top, where,
			"story", "scope_id", "account_id", "region", "arn", "finding_kinds",
			"candidates_count", "ready_count", "refused_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis", "analysis_status",
			"limitations", "terraform_import_plan", "candidates")
		requireDescribed(t, top, where,
			"story", "scope_id", "account_id", "region", "arn", "finding_kinds",
			"candidates_count", "ready_count", "refused_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis", "analysis_status",
			"limitations", "terraform_import_plan", "candidates")
		plan := propsOf(t, top["terraform_import_plan"].(map[string]any), where+" terraform_import_plan")
		// Owner: iac.terraformImportPlanArtifact via buildTerraformImportPlanCandidates.
		requireExactKeys(t, plan, where+" terraform_import_plan", "format", "hcl")
		requireDescribed(t, plan, where+" terraform_import_plan", "format", "hcl")
		items := itemsOf(t, top["candidates"].(map[string]any), where+" candidates")
		cprops := propsOf(t, items, where+" candidates[]")
		// Owner: iac.terraformImportPlanCandidate via terraformImportPlanCandidateForFinding.
		requireExactKeys(t, cprops, where+" candidates[]",
			"id", "finding_id", "status", "provider", "account_id", "region", "arn",
			"cloud_resource_type", "terraform_resource_type", "import_id",
			"suggested_resource_address", "destination_hint", "configuration_shape",
			"provider_hint", "warnings", "refusal_reasons", "import_block",
			"config_shape_hint", "evidence_refs", "safety_gate")
		requireDescribed(t, cprops, where+" candidates[]",
			"id", "finding_id", "status", "provider", "account_id", "region", "arn",
			"cloud_resource_type", "terraform_resource_type", "import_id",
			"suggested_resource_address", "destination_hint", "configuration_shape",
			"provider_hint", "warnings", "refusal_reasons", "import_block",
			"config_shape_hint", "evidence_refs", "safety_gate")
		hint := propsOf(t, cprops["provider_hint"].(map[string]any), where+" provider_hint")
		// Owner: iac.terraformProviderHint.
		requireExactKeys(t, hint, where+" provider_hint",
			"provider", "account_id", "region", "alias")
		requireDescribed(t, hint, where+" provider_hint",
			"provider", "account_id", "region", "alias")
		csh := propsOf(t, cprops["config_shape_hint"].(map[string]any), where+" config_shape_hint")
		// Owner: iac.terraformConfigShapeHint via config_shape_hints.go.
		// Owner: iac.terraformConfigShapeHint; hcl_skeleton and
		// manual_fill_warnings are non-omitempty alongside format,
		// resource_address, and required_arguments.
		requireExactKeys(t, csh, where+" config_shape_hint",
			"format", "resource_address", "provider_alias", "required_arguments",
			"notable_optional_arguments", "omitted_sensitive_arguments",
			"hcl_skeleton", "manual_fill_warnings")
		requireDescribed(t, csh, where+" config_shape_hint",
			"format", "resource_address", "provider_alias", "required_arguments",
			"notable_optional_arguments", "omitted_sensitive_arguments",
			"hcl_skeleton", "manual_fill_warnings")
		gate := propsOf(t, cprops["safety_gate"].(map[string]any), where+" safety_gate")
		requireExactKeys(t, gate, where+" safety_gate", safetyGateKeys...)
		requireDescribed(t, gate, where+" safety_gate", safetyGateKeys...)
	})

	t.Run("iac management status", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/iac/management-status"
		top := propsOf(t, schema200(t, "/api/v0/iac/management-status", "post"), where)
		requireExactKeys(t, top, where,
			"story", "arn", "scope_id", "account_id", "region", "management_status",
			"analysis_status", "finding", "safety_gate", "total_findings_count",
			"limitations")
		requireDescribed(t, top, where,
			"story", "arn", "scope_id", "account_id", "region", "management_status",
			"analysis_status", "finding", "safety_gate", "total_findings_count",
			"limitations")
		// Owner: *iac.ManagementFindingRow via loadExactIaCManagementFinding
		// (null when no active finding). Contract pins the object-or-null
		// shape plus its description instead of exact keys.
		finding := top["finding"].(map[string]any)
		if _, ok := finding["description"]; !ok {
			t.Errorf("%s: finding must carry a description", where)
		}
		requireKeys(t, top, where, "finding", "safety_gate")
		gate := propsOf(t, top["safety_gate"].(map[string]any), where+" safety_gate")
		// Owner: packet.ManagementSafetyGate via finding.SafetyGate (or the
		// empty gate when no finding is active).
		requireExactKeys(t, gate, where+" safety_gate", safetyGateKeys...)
		requireDescribed(t, gate, where+" safety_gate", safetyGateKeys...)
	})

	t.Run("iac management status explanation", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/iac/management-status/explain"
		top := propsOf(t, schema200(t, "/api/v0/iac/management-status/explain", "post"), where)
		requireExactKeys(t, top, where,
			"story", "arn", "scope_id", "account_id", "region", "finding",
			"evidence_groups", "safety_gate", "total_findings_count", "limitations")
		requireDescribed(t, top, where,
			"story", "arn", "scope_id", "account_id", "region", "finding",
			"evidence_groups", "safety_gate", "total_findings_count", "limitations")
		finding := top["finding"].(map[string]any)
		if _, ok := finding["description"]; !ok {
			t.Errorf("%s: finding must carry a description", where)
		}
		groups := itemsOf(t, top["evidence_groups"].(map[string]any), where+" evidence_groups")
		gprops := propsOf(t, groups, where+" evidence_groups[]")
		// Owner: iac.managementEvidenceGroup via managementEvidenceGroups.
		requireExactKeys(t, gprops, where+" evidence_groups[]", "layer", "count", "evidence")
		requireDescribed(t, gprops, where+" evidence_groups[]", "layer", "count", "evidence")
		ev := itemsOf(t, gprops["evidence"].(map[string]any), where+" evidence")
		eprops := propsOf(t, ev, where+" evidence[]")
		// Owner: iac.ManagementEvidenceRow via sanitizeIaCManagementEvidence.
		requireExactKeys(t, eprops, where+" evidence[]",
			"id", "source_system", "evidence_type", "scope_id", "key", "value",
			"confidence", "provenance_only")
		requireDescribed(t, eprops, where+" evidence[]",
			"id", "source_system", "evidence_type", "scope_id", "key", "value",
			"confidence", "provenance_only")
		requireKeys(t, top, where, "evidence_groups", "safety_gate")
		gate := propsOf(t, top["safety_gate"].(map[string]any), where+" safety_gate")
		// Owner: packet.ManagementSafetyGate via managementSafetyGateForExactFinding.
		requireExactKeys(t, gate, where+" safety_gate", safetyGateKeys...)
		requireDescribed(t, gate, where+" safety_gate", safetyGateKeys...)
	})
}
