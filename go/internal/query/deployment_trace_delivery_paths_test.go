package query

import "testing"

func TestBuildNormalizedDeliveryPathsFiltersEmptyAndDeduplicates(t *testing.T) {
	t.Parallel()

	got := buildNormalizedDeliveryPaths(
		[]map[string]any{
			{
				"repo_id":    "repo-helm",
				"repo_name":  "deployment-helm",
				"confidence": 0.98,
			},
		},
		[]map[string]any{
			{
				"id":         "cloud-1",
				"name":       "payments-db",
				"kind":       "rds_instance",
				"confidence": 0.91,
			},
		},
		[]map[string]any{
			{
				"entity_id":   "k8s-1",
				"entity_name": "payments-api",
				"kind":        "Deployment",
			},
		},
		[]string{"ghcr.io/acme/payments-api:1.2.3"},
		[]map[string]any{
			{
				"type":        "USES_IMAGE",
				"target_name": "ghcr.io/acme/payments-api:1.2.3",
				"source_name": "payments-api",
				"reason":      "k8s_container_image",
			},
		},
		map[string]any{
			"delivery_paths": []map[string]any{
				{
					"type":          "deployment_source",
					"target":        "deployment-helm",
					"target_id":     "repo-helm",
					"confidence":    0.98,
					"repo_name":     "deployment-helm",
					"artifact_type": "",
				},
				{
					"kind":          "workflow_artifact",
					"path":          ".github/workflows/deploy.yaml",
					"workflow_name": "deploy",
				},
				{
					"kind": "workflow_artifact",
					"path": ".github/workflows/deploy.yaml",
				},
				{},
			},
		},
	)

	if gotCount, want := len(got), 6; gotCount != want {
		t.Fatalf("len(buildNormalizedDeliveryPaths()) = %d, want %d; rows=%#v", gotCount, want, got)
	}

	for _, row := range got {
		if StringVal(row, "type") == "" {
			t.Fatalf("normalized row missing type: %#v", row)
		}
		if StringVal(row, "type") == "repository_delivery_artifact" && StringVal(row, "path") == "" && StringVal(row, "kind") == "" && StringVal(row, "artifact_type") == "" && StringVal(row, "evidence_kind") == "" {
			t.Fatalf("repository delivery artifact row carries no usable identity: %#v", row)
		}
		if StringVal(row, "type") == "" && StringVal(row, "target") == "" && StringVal(row, "path") == "" && StringVal(row, "kind") == "" && StringVal(row, "artifact_type") == "" && StringVal(row, "evidence_kind") == "" {
			t.Fatalf("structurally empty row leaked into normalized output: %#v", row)
		}
	}

	if got, want := StringVal(got[0], "type"), "deployment_source"; got != want {
		t.Fatalf("normalized[0].type = %q, want %q", got, want)
	}
	if got, want := StringVal(got[0], "target"), "deployment-helm"; got != want {
		t.Fatalf("normalized[0].target = %q, want %q", got, want)
	}
	if got, want := StringVal(got[5], "type"), "repository_delivery_artifact"; got != want {
		t.Fatalf("normalized[4].type = %q, want %q", got, want)
	}
	if got, want := StringVal(got[5], "path"), ".github/workflows/deploy.yaml"; got != want {
		t.Fatalf("normalized[4].path = %q, want %q", got, want)
	}
}
