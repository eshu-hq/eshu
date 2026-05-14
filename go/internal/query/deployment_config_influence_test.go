package query

import "testing"

func TestBuildDeploymentConfigInfluenceResponseReturnsPromptReadyFiles(t *testing.T) {
	t.Parallel()

	workloadContext := map[string]any{
		"id":        "workload:eshu-hqgraph-resolution-engine",
		"name":      "eshu-hqgraph-resolution-engine",
		"kind":      "service",
		"repo_id":   "repo-runtime",
		"repo_name": "eshu",
		"deployment_sources": []map[string]any{{
			"repo_id":   "repo-gitops",
			"repo_name": "platform-gitops",
			"reason":    "helm values reference",
		}},
		"deployment_evidence": map[string]any{
			"artifacts": []map[string]any{
				{
					"source_repo_id":   "repo-gitops",
					"source_repo_name": "platform-gitops",
					"path":             "clusters/ops-qa/eshu/values.yaml",
					"artifact_family":  "helm",
					"evidence_kind":    "helm_values_reference",
					"matched_alias":    "image.tag",
					"matched_value":    "ghcr.io/eshu-hq/eshu:1.2.3",
					"environment":      "ops-qa",
					"start_line":       17,
					"end_line":         18,
					"resolved_id":      "rel-image",
				},
				{
					"source_repo_id":   "repo-gitops",
					"source_repo_name": "platform-gitops",
					"path":             "charts/eshu/templates/deployment.yaml",
					"artifact_family":  "helm",
					"evidence_kind":    "kubernetes_resource_limit",
					"matched_alias":    "resources.limits.cpu",
					"matched_value":    "500m",
					"environment":      "ops-qa",
					"start_line":       44,
					"end_line":         47,
					"resolved_id":      "rel-limit",
				},
				{
					"source_repo_id":   "repo-gitops",
					"source_repo_name": "platform-gitops",
					"path":             "clusters/ops-qa/eshu/env.yaml",
					"artifact_family":  "argocd",
					"evidence_kind":    "runtime_config_reference",
					"matched_alias":    "env.ESHU_REDUCER_WORKERS",
					"matched_value":    "8",
					"environment":      "ops-qa",
				},
			},
		},
		"k8s_resources": []map[string]any{{
			"id":        "k8s:deployment:resolution-engine",
			"name":      "eshu-hqgraph-resolution-engine",
			"kind":      "Deployment",
			"namespace": "eshu",
		}},
		"image_refs": []string{"ghcr.io/eshu-hq/eshu:1.2.3"},
	}

	resp := buildDeploymentConfigInfluenceResponse(deploymentConfigInfluenceRequest{
		ServiceName: "eshu-hqgraph-resolution-engine",
		Environment: "ops-qa",
		Limit:       10,
	}, workloadContext)

	if got := StringVal(resp, "service_name"); got != "eshu-hqgraph-resolution-engine" {
		t.Fatalf("service_name = %q, want eshu-hqgraph-resolution-engine", got)
	}
	for key := range map[string]struct{}{
		"image_tag_sources":       {},
		"runtime_setting_sources": {},
		"resource_limit_sources":  {},
		"values_layers":           {},
		"rendered_targets":        {},
		"read_first_files":        {},
	} {
		rows := mapSliceValue(resp, key)
		if len(rows) == 0 {
			t.Fatalf("%s is empty, want prompt-ready rows", key)
		}
	}

	readFirst := mapSliceValue(resp, "read_first_files")
	if got := StringVal(readFirst[0], "repo_id"); got == "" {
		t.Fatalf("read_first_files[0].repo_id is empty")
	}
	if got := StringVal(readFirst[0], "relative_path"); got == "" || got[0] == '/' {
		t.Fatalf("read_first_files[0].relative_path = %q, want portable relative path", got)
	}
	if got := StringVal(readFirst[0], "next_call"); got != "get_file_lines" {
		t.Fatalf("read_first_files[0].next_call = %q, want get_file_lines", got)
	}

	coverage := mapValue(resp, "coverage")
	if got := StringVal(coverage, "query_shape"); got != "deployment_config_influence_story" {
		t.Fatalf("coverage.query_shape = %q, want deployment_config_influence_story", got)
	}
	if BoolVal(coverage, "truncated") {
		t.Fatalf("coverage.truncated = true, want false")
	}
	if got := StringSliceVal(resp, "recommended_next_calls"); len(got) == 0 {
		t.Fatalf("recommended_next_calls is empty")
	}
}
