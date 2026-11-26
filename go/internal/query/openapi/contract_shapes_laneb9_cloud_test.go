// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB9Cloud pins the lane-B9 shape-completeness fixes
// for the cloud family (issue #11491 follow-up): the AWS and multi-cloud
// runtime drift findings pages, the canonical cloud inventory readback, the
// graph-backed cloud resource list, and the SSM host inventory must name
// their 200-response properties instead of bare {"type": "object"} shapes,
// and every property carries a description naming its Go owner. Each entry
// names the handler, projector, or row struct that emits the keys the
// contract asserts.
func TestContractShapesLaneB9Cloud(t *testing.T) {
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

	t.Run("aws runtime drift findings", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/aws/runtime-drift/findings"
		top := propsOf(t, schema200(t, "/api/v0/aws/runtime-drift/findings", "post"), where)
		requireExactKeys(t, top, where,
			"scope_id", "account_id", "region", "arn", "story", "finding_kinds",
			"outcome_groups", "drift_findings", "findings_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis", "analysis_status",
			"graph_projection_note", "limitations")
		requireDescribed(t, top, where,
			"scope_id", "account_id", "region", "arn", "story", "finding_kinds",
			"outcome_groups", "drift_findings", "findings_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis", "analysis_status",
			"graph_projection_note", "limitations")
		requireScalarTypes(t, top, where, map[string]string{
			"scope_id": "string", "findings_count": "integer", "truncated": "boolean",
		})
		groups := itemsOf(t, top["outcome_groups"].(map[string]any), where+" outcome_groups")
		gprops := propsOf(t, groups, where+" outcome_groups[]")
		// Owner: iac.awsRuntimeDriftOutcomeGroup via awsRuntimeDriftOutcomeGroups.
		requireExactKeys(t, gprops, where+" outcome_groups[]", "outcome", "count", "arns")
		requireDescribed(t, gprops, where+" outcome_groups[]", "outcome", "count", "arns")
		items := itemsOf(t, top["drift_findings"].(map[string]any), where+" drift_findings")
		fprops := propsOf(t, items, where+" drift_findings[]")
		// Owner: iac.ManagementFindingRow plus AWSRuntimeDriftFindingRow.Outcome/
		// PromotionOutcome/PromotionReason via awsRuntimeDriftFindingRows.
		requireKeys(t, fprops, where+" drift_findings[]",
			"id", "provider", "finding_kind", "management_status", "confidence",
			"safety_gate", "evidence", "drifted_attributes")
		requireDescribed(t, fprops, where+" drift_findings[]",
			"id", "arn", "provider", "account_id", "region", "finding_kind",
			"management_status", "outcome", "promotion_outcome", "promotion_reason",
			"confidence", "scope_id", "generation_id", "source_system",
			"service_candidates", "environment_candidates", "dependency_paths",
			"missing_evidence", "warning_flags", "recommended_action",
			"safety_gate", "evidence", "drifted_attributes")
		gate := propsOf(t, fprops["safety_gate"].(map[string]any), where+" safety_gate")
		// Owner: packet.ManagementSafetyGate via iacManagementSafetyGate.
		requireExactKeys(t, gate, where+" safety_gate", safetyGateKeys...)
		requireDescribed(t, gate, where+" safety_gate", safetyGateKeys...)
		ev := itemsOf(t, fprops["evidence"].(map[string]any), where+" evidence")
		eprops := propsOf(t, ev, where+" evidence[]")
		// Owner: iac.ManagementEvidenceRow.
		requireExactKeys(t, eprops, where+" evidence[]",
			"id", "source_system", "evidence_type", "scope_id", "key", "value",
			"confidence", "provenance_only")
		requireDescribed(t, eprops, where+" evidence[]",
			"id", "source_system", "evidence_type", "scope_id", "key", "value",
			"confidence", "provenance_only")
		da := itemsOf(t, fprops["drifted_attributes"].(map[string]any), where+" drifted_attributes")
		daprops := propsOf(t, da, where+" drifted_attributes[]")
		// Owner: querycontract.DriftedAttributeView.
		requireExactKeys(t, daprops, where+" drifted_attributes[]", driftedAttrKeys...)
		requireDescribed(t, daprops, where+" drifted_attributes[]", driftedAttrKeys...)
	})

	t.Run("multi-cloud runtime drift findings", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/cloud/runtime-drift/findings"
		top := propsOf(t, schema200(t, "/api/v0/cloud/runtime-drift/findings", "post"), where)
		requireExactKeys(t, top, where,
			"scope_id", "provider", "cloud_resource_uid", "finding_kinds", "story",
			"source_state_groups", "drift_findings", "findings_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis", "analysis_status",
			"limitations")
		requireDescribed(t, top, where,
			"scope_id", "provider", "cloud_resource_uid", "finding_kinds", "story",
			"source_state_groups", "drift_findings", "findings_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis", "analysis_status",
			"limitations")
		requireScalarTypes(t, top, where, map[string]string{
			"scope_id": "string", "findings_count": "integer", "truncated": "boolean",
		})
		groups := itemsOf(t, top["source_state_groups"].(map[string]any), where+" source_state_groups")
		gprops := propsOf(t, groups, where+" source_state_groups[]")
		// Owner: query.cloudRuntimeDriftSourceStateGroup via cloudRuntimeDriftSourceStateGroups.
		requireExactKeys(t, gprops, where+" source_state_groups[]",
			"source_state", "count", "cloud_resource_uids")
		requireDescribed(t, gprops, where+" source_state_groups[]",
			"source_state", "count", "cloud_resource_uids")
		items := itemsOf(t, top["drift_findings"].(map[string]any), where+" drift_findings")
		fprops := propsOf(t, items, where+" drift_findings[]")
		// Owner: packet.CloudRuntimeDriftFindingView via cloudRuntimeDriftFindingViews.
		requireExactKeys(t, fprops, where+" drift_findings[]",
			"fact_id", "provider", "scope_id", "generation_id", "source_system",
			"cloud_resource_uid", "finding_kind", "management_status", "confidence",
			"source_state", "matched_terraform_state_address", "missing_evidence",
			"recommended_action", "drifted_attributes", "safety_gate")
		requireDescribed(t, fprops, where+" drift_findings[]",
			"fact_id", "provider", "scope_id", "generation_id", "source_system",
			"cloud_resource_uid", "finding_kind", "management_status", "confidence",
			"source_state", "matched_terraform_state_address", "missing_evidence",
			"recommended_action", "drifted_attributes", "safety_gate")
		gate := propsOf(t, fprops["safety_gate"].(map[string]any), where+" safety_gate")
		// Owner: packet.ManagementSafetyGate via iacManagementSafetyGate.
		requireExactKeys(t, gate, where+" safety_gate", safetyGateKeys...)
		requireDescribed(t, gate, where+" safety_gate", safetyGateKeys...)
		da := itemsOf(t, fprops["drifted_attributes"].(map[string]any), where+" drifted_attributes")
		daprops := propsOf(t, da, where+" drifted_attributes[]")
		// Owner: querycontract.DriftedAttributeView.
		requireExactKeys(t, daprops, where+" drifted_attributes[]", driftedAttrKeys...)
		requireDescribed(t, daprops, where+" drifted_attributes[]", driftedAttrKeys...)
	})

	t.Run("cloud resource inventory", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/cloud/inventory"
		top := propsOf(t, schema200(t, "/api/v0/cloud/inventory", "get"), where)
		requireExactKeys(t, top, where,
			"resources", "count", "limit", "truncated", "next_cursor", "scope", "warning_flags")
		requireDescribed(t, top, where,
			"resources", "count", "limit", "truncated", "next_cursor", "scope", "warning_flags")
		items := itemsOf(t, top["resources"].(map[string]any), where+" resources")
		rprops := propsOf(t, items, where+" resources[]")
		// Owner: query.cloudInventoryResourceView projecting the canonical
		// reducer_cloud_resource_identity payload.
		requireKeys(t, rprops, where+" resources[]",
			"cloud_resource_uid", "provider", "resource_type", "management_origin",
			"scope_id", "generation_id", "source_state", "evidence")
		requireDescribed(t, rprops, where+" resources[]",
			"cloud_resource_uid", "provider", "resource_type", "management_origin",
			"scope_id", "generation_id", "source_state", "tag_value_fingerprints",
			"attributes", "code_sha256_correlation", "identity_policy_evidence",
			"identity_policy_evidence_truncated", "resource_change_freshness",
			"resource_change_freshness_truncated", "evidence")
		corr := propsOf(t, rprops["code_sha256_correlation"].(map[string]any), where+" code_sha256_correlation")
		// Owner: query.cloudInventoryCodeCorrelationLabel.
		requireExactKeys(t, corr, where+" code_sha256_correlation[]",
			"status", "truth_basis", "unsupported_reason")
		requireDescribed(t, corr, where+" code_sha256_correlation[]",
			"status", "truth_basis", "unsupported_reason")
		ev := propsOf(t, rprops["evidence"].(map[string]any), where+" evidence")
		// Owner: query.cloudInventoryResourceView per-layer evidence flags.
		requireExactKeys(t, ev, where+" evidence", "declared", "applied", "observed")
		requireDescribed(t, ev, where+" evidence", "declared", "applied", "observed")
		ipe := itemsOf(t, rprops["identity_policy_evidence"].(map[string]any), where+" identity_policy_evidence")
		iprops := propsOf(t, ipe, where+" identity_policy_evidence[]")
		requireKeys(t, iprops, where+" identity_policy_evidence[]",
			"evidence_key", "identity_type", "role_class", "principal_fingerprint",
			"client_fingerprint", "object_fingerprint", "tenant_fingerprint")
		requireDescribed(t, iprops, where+" identity_policy_evidence[]",
			"evidence_key", "identity_type", "role_class", "principal_fingerprint",
			"client_fingerprint", "object_fingerprint", "tenant_fingerprint")
		rcf := itemsOf(t, rprops["resource_change_freshness"].(map[string]any), where+" resource_change_freshness")
		rprops2 := propsOf(t, rcf, where+" resource_change_freshness[]")
		requireKeys(t, rprops2, where+" resource_change_freshness[]",
			"evidence_key", "change_type", "change_time", "operation", "client_type",
			"actor_class", "actor_fingerprint", "changed_property_paths",
			"changed_property_truncated", "tombstone_candidate")
		requireDescribed(t, rprops2, where+" resource_change_freshness[]",
			"evidence_key", "change_type", "change_time", "operation", "client_type",
			"actor_class", "actor_fingerprint", "changed_property_paths",
			"changed_property_truncated", "tombstone_candidate")
		// Filter-echo string map: query.cloudInventoryScope echoes the caller
		// filter keys (provider, scope_id or the account alias used,
		// management_origin); keys vary per request so the contract pins the
		// map shape plus its description instead of exact keys.
		scope := top["scope"].(map[string]any)
		if _, ok := scope["additionalProperties"]; !ok {
			t.Errorf("%s: scope must carry additionalProperties for the filter-echo map", where)
		}
		requireDescribed(t, top, where, "scope")
	})

	t.Run("cloud resource list", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/cloud/resources"
		top := propsOf(t, schema200(t, "/api/v0/cloud/resources", "get"), where)
		requireExactKeys(t, top, where,
			"resources", "count", "limit", "truncated", "next_cursor", "scope")
		requireDescribed(t, top, where,
			"resources", "count", "limit", "truncated", "next_cursor", "scope")
		items := itemsOf(t, top["resources"].(map[string]any), where+" resources")
		rprops := propsOf(t, items, where+" resources[]")
		// Owner: query.CloudResourceRow hydrated from the authoritative graph.
		requireExactKeys(t, rprops, where+" resources[]",
			"id", "resource_type", "name", "provider", "region", "account_id",
			"arn", "service_name", "state")
		requireDescribed(t, rprops, where+" resources[]",
			"id", "resource_type", "name", "provider", "region", "account_id",
			"arn", "service_name", "state")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		// Owner: query.writeCloudResourceListResponse keyset cursor.
		requireExactKeys(t, cursor, where+" next_cursor", "after_resource_type", "after_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_resource_type", "after_id")
		// Filter-echo string map: query.cloudResourceListScope echoes the
		// caller filter keys; keys vary per request so the contract pins the
		// map shape plus its description instead of exact keys.
		scope := top["scope"].(map[string]any)
		if _, ok := scope["additionalProperties"]; !ok {
			t.Errorf("%s: scope must carry additionalProperties for the filter-echo map", where)
		}
		requireDescribed(t, top, where, "scope")
	})

	t.Run("ssm host inventory", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/cloud/ssm-host-inventory"
		top := propsOf(t, schema200(t, "/api/v0/cloud/ssm-host-inventory", "get"), where)
		requireExactKeys(t, top, where, "hosts", "malformed_facts", "next_cursor")
		requireDescribed(t, top, where, "hosts", "malformed_facts", "next_cursor")
		items := itemsOf(t, top["hosts"].(map[string]any), where+" hosts")
		hprops := propsOf(t, items, where+" hosts[]")
		// Owner: query.ssmHostView via ssmHostInventoryResponse.
		requireExactKeys(t, hprops, where+" hosts[]",
			"partition", "account_id", "region", "instance_id", "status", "status_reason",
			"capture_time", "scope_id", "generation_id", "applications",
			"applications_truncated", "platforms", "facts_truncated")
		requireDescribed(t, hprops, where+" hosts[]",
			"partition", "account_id", "region", "instance_id", "status", "status_reason",
			"capture_time", "scope_id", "generation_id", "applications",
			"applications_truncated", "platforms", "facts_truncated")
		apps := itemsOf(t, hprops["applications"].(map[string]any), where+" applications")
		aprops := propsOf(t, apps, where+" applications[]")
		// Owner: query.ssm_host_inventory_overlay application projection.
		requireExactKeys(t, aprops, where+" applications[]",
			"uid", "name", "version", "release", "epoch", "package_id",
			"architecture", "capture_time")
		requireDescribed(t, aprops, where+" applications[]",
			"uid", "name", "version", "release", "epoch", "package_id",
			"architecture", "capture_time")
		plats := itemsOf(t, hprops["platforms"].(map[string]any), where+" platforms")
		pprops := propsOf(t, plats, where+" platforms[]")
		// Owner: query.ssm_host_inventory_overlay platform projection.
		requireExactKeys(t, pprops, where+" platforms[]",
			"uid", "source_type", "platform_type", "platform_name", "platform_version",
			"capture_time", "observation_time")
		requireDescribed(t, pprops, where+" platforms[]",
			"uid", "source_type", "platform_type", "platform_name", "platform_version",
			"capture_time", "observation_time")
	})
}
