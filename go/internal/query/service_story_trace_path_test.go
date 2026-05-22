package query

import "testing"

func TestBuildServiceStoryResponseIncludesCodeToRuntimeTrace(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	ctx["deployment_evidence"] = map[string]any{
		"artifacts": []map[string]any{
			{
				"relationship_type": "DEPLOYS_FROM",
				"resolved_id":       "resolved-gitops",
				"source_repo_name":  "deployment-charts",
				"path":              "argocd/prod/app.yaml",
				"artifact_family":   "argocd",
			},
		},
		"delivery_workflows": []map[string]any{
			{
				"tool_family": "github_actions",
				"path":        ".github/workflows/deploy.yml",
			},
		},
		"delivery_paths": []map[string]any{
			{
				"tool_family":       "kubernetes",
				"path":              "k8s/deployment.yaml",
				"container_images":  []string{"ghcr.io/acme/sample-service-api:1.2.3"},
				"runtime_platform":  "eks-prod",
				"relationship_type": "DEPLOYS_FROM",
			},
		},
		"shared_config_paths": []map[string]any{
			{
				"path":        "config/prod.yaml",
				"tool_family": "kustomize",
			},
		},
	}

	got := buildServiceStoryResponse("sample-service-api", ctx)
	trace := mapValue(got, "code_to_runtime_trace")
	if got, want := StringVal(trace, "status"), "partial"; got != want {
		t.Fatalf("code_to_runtime_trace.status = %q, want %q", got, want)
	}
	segments := mapSliceValue(trace, "segments")
	for _, want := range []string{
		"service_identity",
		"code_entrypoints",
		"ci_cd",
		"image_package",
		"deployment_config",
		"runtime",
		"cloud_dependencies",
	} {
		if segmentByName(segments, want) == nil {
			t.Fatalf("code_to_runtime_trace.segments missing %q: %#v", want, segments)
		}
	}
	if got, want := StringVal(segmentByName(segments, "runtime"), "status"), "exact"; got != want {
		t.Fatalf("runtime segment status = %q, want %q", got, want)
	}
	imagePackage := segmentByName(segments, "image_package")
	if got, want := StringVal(imagePackage, "status"), "derived"; got != want {
		t.Fatalf("image_package segment status = %q, want %q", got, want)
	}
	evidence := mapSliceValue(imagePackage, "evidence")
	if len(evidence) != 1 {
		t.Fatalf("image_package evidence = %#v, want one image/package row", evidence)
	}
	if got, want := StringVal(evidence[0], "image_ref"), "ghcr.io/acme/sample-service-api:1.2.3"; got != want {
		t.Fatalf("image_package evidence image_ref = %q, want %q", got, want)
	}
	cloud := segmentByName(segments, "cloud_dependencies")
	if got, want := StringVal(cloud, "status"), "missing_evidence"; got != want {
		t.Fatalf("cloud_dependencies segment status = %q, want %q", got, want)
	}
	missing := StringSliceVal(trace, "missing_segments")
	if !stringSliceContains(missing, "cloud_dependencies") {
		t.Fatalf("missing_segments = %#v, want cloud_dependencies", missing)
	}
}

func segmentByName(segments []map[string]any, name string) map[string]any {
	for _, segment := range segments {
		if StringVal(segment, "name") == name {
			return segment
		}
	}
	return nil
}
