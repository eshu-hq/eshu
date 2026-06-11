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
