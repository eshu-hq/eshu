// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestCloudInventoryResourceViewSurfacesTagFingerprints proves the readback
// projection surfaces keyed tag value fingerprints when the canonical payload
// carries them, so callers can correlate by shared tag value without the tag
// value text ever crossing the wire.
func TestCloudInventoryResourceViewSurfacesTagFingerprints(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "cloud:tenant-1",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:abc",
			"provider":              "azure",
			"resource_type":         "Microsoft.Compute/virtualMachines",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"tag_value_fingerprints": map[string]any{
				"env":   "az-env-marker",
				"owner": "az-owner-marker",
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	fingerprints, ok := view["tag_value_fingerprints"].(map[string]string)
	if !ok {
		t.Fatalf("tag_value_fingerprints type = %T, want map[string]string", view["tag_value_fingerprints"])
	}
	if len(fingerprints) != 2 || fingerprints["env"] != "az-env-marker" || fingerprints["owner"] != "az-owner-marker" {
		t.Fatalf("tag_value_fingerprints = %#v, want env/owner markers", fingerprints)
	}
	for key, marker := range fingerprints {
		if marker == "prod" || marker == "payments-team" {
			t.Fatalf("raw tag value leaked for key %q: %q", key, marker)
		}
	}
}

// TestCloudInventoryResourceViewOmitsTagsWhenAbsent proves an untagged resource
// (e.g. the AWS/GCP path with no tag evidence) carries no tag fingerprints field.
func TestCloudInventoryResourceViewOmitsTagsWhenAbsent(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:aws-1",
			"provider":              "aws",
			"resource_type":         "AWS::S3::Bucket",
			"management_origin":     "declared",
			"has_declared_evidence": true,
		},
	}

	view := cloudInventoryResourceView(envelope)
	if _, present := view["tag_value_fingerprints"]; present {
		t.Fatalf("tag_value_fingerprints present for untagged resource: %#v", view)
	}
}

// TestCloudInventoryResourceViewSurfacesBoundedIdentityPolicyEvidence proves
// the readback can show Azure policy evidence without raw provider locators,
// raw principal GUIDs, or raw assignment scopes.
func TestCloudInventoryResourceViewSurfacesBoundedIdentityPolicyEvidence(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "azure:tenant:subscription:sub-1:all:all:resource_graph",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:azure-1",
			"provider":              "azure",
			"resource_type":         "Microsoft.Compute/virtualMachines",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"raw_identity":          "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
			"identity_policy_evidence": []any{
				map[string]any{
					"evidence_key":          "identity-stable-1",
					"identity_type":         "system_assigned",
					"role_class":            "contributor",
					"principal_fingerprint": "principal-marker",
					"tenant_fingerprint":    "tenant-marker",
					"assignment_scope":      "/subscriptions/sub-1/resourceGroups/rg",
				},
			},
			"identity_policy_evidence_truncated": true,
		},
	}

	view := cloudInventoryResourceView(envelope)
	evidence, ok := view["identity_policy_evidence"].([]map[string]string)
	if !ok {
		t.Fatalf("identity_policy_evidence type = %T, want []map[string]string", view["identity_policy_evidence"])
	}
	if len(evidence) != 1 {
		t.Fatalf("identity_policy_evidence length = %d, want 1", len(evidence))
	}
	row := evidence[0]
	if row["identity_type"] != "system_assigned" || row["principal_fingerprint"] != "principal-marker" {
		t.Fatalf("identity_policy_evidence row = %#v", row)
	}
	for _, forbidden := range []string{"raw_identity", "assignment_scope", "principal_id", "client_id", "object_id", "tenant_id"} {
		if _, present := row[forbidden]; present {
			t.Fatalf("forbidden field %q present in identity policy evidence: %#v", forbidden, row)
		}
	}
	if view["identity_policy_evidence_truncated"] != true {
		t.Fatalf("identity_policy_evidence_truncated = %#v, want true", view["identity_policy_evidence_truncated"])
	}
}

// TestCloudInventoryResourceViewSurfacesBoundedResourceChangeFreshness proves
// Azure change evidence can be surfaced as freshness evidence without raw
// provider targets, raw actor ids, or final-state tombstone claims.
func TestCloudInventoryResourceViewSurfacesBoundedResourceChangeFreshness(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "azure:tenant:subscription:sub-1:all:all:resourcechanges",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:azure-1",
			"provider":              "azure",
			"resource_type":         "Microsoft.Compute/virtualMachines",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"raw_identity":          "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
			"resource_change_freshness": []any{
				map[string]any{
					"evidence_key":               "change-stable-1",
					"change_type":                "deleted",
					"change_time":                "2026-06-16T10:30:00Z",
					"operation":                  "Microsoft.Compute/virtualMachines/delete",
					"client_type":                "AzurePortal",
					"actor_class":                "user",
					"actor_fingerprint":          "actor-marker",
					"changed_property_paths":     []any{"properties.provisioningState"},
					"changed_property_truncated": true,
					"tombstone_candidate":        true,
					"target_arm_resource_id":     "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
					"changedBy":                  "raw-actor",
					"previousValue":              "raw-old",
					"newValue":                   "raw-new",
				},
			},
			"resource_change_freshness_truncated": true,
		},
	}

	view := cloudInventoryResourceView(envelope)
	evidence, ok := view["resource_change_freshness"].([]map[string]any)
	if !ok {
		t.Fatalf("resource_change_freshness type = %T, want []map[string]any", view["resource_change_freshness"])
	}
	if len(evidence) != 1 {
		t.Fatalf("resource_change_freshness length = %d, want 1", len(evidence))
	}
	row := evidence[0]
	if row["change_type"] != "deleted" || row["tombstone_candidate"] != true {
		t.Fatalf("resource change freshness row = %#v", row)
	}
	for _, forbidden := range []string{"raw_identity", "target_arm_resource_id", "changedBy", "previousValue", "newValue"} {
		if _, present := row[forbidden]; present {
			t.Fatalf("forbidden field %q present in resource change freshness: %#v", forbidden, row)
		}
	}
	if view["resource_change_freshness_truncated"] != true {
		t.Fatalf("resource_change_freshness_truncated = %#v, want true", view["resource_change_freshness_truncated"])
	}
}
