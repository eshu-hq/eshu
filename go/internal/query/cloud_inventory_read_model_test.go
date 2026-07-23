// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"
)

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

// TestCloudInventoryResourceViewSurfacesAttributes proves the readback
// projection surfaces the attributes map when present, omits it when absent,
// and drops any nested-map value so no unexpected structured content leaks.
func TestCloudInventoryResourceViewSurfacesAttributes(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "gcp:org:eshu:project:prod",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:bq-1",
			"provider":              "gcp",
			"resource_type":         "bigquery.googleapis.com/Table",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"attributes": map[string]any{
				"table_type":         "TABLE",
				"schema_field_count": float64(12),
				"clustering_fields":  []any{"project_id", "date"},
				"nested_drop":        map[string]any{"inner": "secret"},
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	raw, present := view["attributes"]
	if !present {
		t.Fatal("attributes key absent from view, want present")
	}
	attrs, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("attributes type = %T, want map[string]any", raw)
	}
	if attrs["table_type"] != "TABLE" {
		t.Fatalf("attributes[table_type] = %#v, want TABLE", attrs["table_type"])
	}
	if attrs["schema_field_count"] != float64(12) {
		t.Fatalf("attributes[schema_field_count] = %#v, want 12", attrs["schema_field_count"])
	}
	fields, ok := attrs["clustering_fields"].([]string)
	if !ok || len(fields) != 2 {
		t.Fatalf("clustering_fields = %#v, want []string{project_id, date}", attrs["clustering_fields"])
	}
	if _, present := attrs["nested_drop"]; present {
		t.Fatalf("nested_drop must be dropped, got %#v", attrs["nested_drop"])
	}
}

// TestCloudInventoryResourceViewOmitsAttributesWhenAbsent proves a resource
// without attributes in the payload produces no attributes key in the view.
func TestCloudInventoryResourceViewOmitsAttributesWhenAbsent(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:compute-1",
			"provider":              "gcp",
			"resource_type":         "compute.googleapis.com/Instance",
			"management_origin":     "observed",
			"has_observed_evidence": true,
		},
	}

	view := cloudInventoryResourceView(envelope)
	if _, present := view["attributes"]; present {
		t.Fatalf("attributes present for resource with no attributes: %#v", view["attributes"])
	}
}

// TestCloudInventoryResourceViewDropsNestedMapFromAttributes is the negative
// test: proves a nested map in the attributes payload is not leaked through
// the view projection.
func TestCloudInventoryResourceViewDropsNestedMapFromAttributes(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:bq-2",
			"provider":              "gcp",
			"resource_type":         "bigquery.googleapis.com/Table",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"attributes": map[string]any{
				"safe_key":   "safe_value",
				"nested_map": map[string]any{"dangerous": "nested"},
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	attrs, ok := view["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes type = %T, want map[string]any", view["attributes"])
	}
	if _, present := attrs["nested_map"]; present {
		t.Fatalf("nested_map leaked through projection: %#v", attrs["nested_map"])
	}
	if attrs["safe_key"] != "safe_value" {
		t.Fatalf("safe_key = %#v, want safe_value", attrs["safe_key"])
	}
}

// TestCloudInventoryResourceViewSurfacesContainersAttribute proves the
// readback projection surfaces the nested "containers" attribute array
// (written by the AWS ECS allowlist, issue #5449) filtered to {image,
// image_digest} per element, and drops a raw sub-key (name) the loader-side
// allowlist would already have removed -- this is the second, independent
// gate the read model applies on top of that filtering.
func TestCloudInventoryResourceViewSurfacesContainersAttribute(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "aws:000000000000",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:ecs-task-1",
			"provider":              "aws",
			"resource_type":         "aws_ecs_task",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"attributes": map[string]any{
				"task_definition_arn": "arn:aws:ecs:us-east-1:000000000000:task-definition/demo:1",
				"containers": []any{
					map[string]any{
						"image":        "000000000000.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
						"image_digest": "sha256:0000000000000000000000000000000000000000000000000000000000aa",
						// name is a raw provider field; it must not survive even if
						// present here, proving the projector's own gate holds
						// independent of the loader.
						"name": "demo-container",
					},
				},
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	attrs, ok := view["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes type = %T, want map[string]any", view["attributes"])
	}
	if attrs["task_definition_arn"] != "arn:aws:ecs:us-east-1:000000000000:task-definition/demo:1" {
		t.Fatalf("task_definition_arn = %#v, want the task definition arn", attrs["task_definition_arn"])
	}
	containers, ok := attrs["containers"].([]map[string]string)
	if !ok || len(containers) != 1 {
		t.Fatalf("containers = %#v, want one filtered container map", attrs["containers"])
	}
	container := containers[0]
	if got, want := container["image"], "000000000000.dkr.ecr.us-east-1.amazonaws.com/demo:latest"; got != want {
		t.Fatalf("containers[0].image = %#v, want %q", got, want)
	}
	if got, want := container["image_digest"], "sha256:0000000000000000000000000000000000000000000000000000000000aa"; got != want {
		t.Fatalf("containers[0].image_digest = %#v, want %q", got, want)
	}
	if _, present := container["name"]; present {
		t.Fatalf("container name must be dropped, got %#v", container["name"])
	}
}

// TestCloudInventoryResourceViewDropsContainersElementsWithNoAllowedKeys is the
// negative test: a containers element carrying only raw keys (no image or
// image_digest) is dropped entirely rather than surfaced as an empty map.
func TestCloudInventoryResourceViewDropsContainersElementsWithNoAllowedKeys(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:ecs-task-2",
			"provider":              "aws",
			"resource_type":         "aws_ecs_task",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"attributes": map[string]any{
				"containers": []any{
					map[string]any{"name": "sidecar", "runtime_id": "0000000000000000000000000000000000000000000000000000000000bb"},
				},
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	if _, present := view["attributes"]; present {
		t.Fatalf("attributes present with no allowlisted content: %#v", view["attributes"])
	}
}

// TestCloudInventoryContainerAttributeKeysIsImageAndDigestOnly pins
// cloudInventoryContainerAttributeKeys to exactly {image, image_digest} (P2
// finding #5, issue #5449). The loader-side allowlist
// (go/internal/storage/postgres/cloud_inventory_evidence.go
// awsCloudInventoryAttributeAllowlist.nestedArrayKeys["containers"]) is
// maintained independently -- there is no shared constant across packages --
// so this test is what catches an accidental widening or narrowing of this
// projector's own container sub-key set.
func TestCloudInventoryContainerAttributeKeysIsImageAndDigestOnly(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{
		"image":        {},
		"image_digest": {},
	}
	if !reflect.DeepEqual(cloudInventoryContainerAttributeKeys, want) {
		t.Fatalf("cloudInventoryContainerAttributeKeys = %#v, want %#v", cloudInventoryContainerAttributeKeys, want)
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
