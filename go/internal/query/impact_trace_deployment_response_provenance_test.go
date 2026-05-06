package query

import (
	"reflect"
	"testing"
)

func TestBuildDeploymentTraceResponsePromotesWorkflowAndControllerProvenance(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload-1",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repo-1",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{
			{
				"instance_id":   "inst-1",
				"platform_name": "sample-service-api-prod",
				"platform_kind": "argocd_application",
				"environment":   "prod",
			},
		},
		"deployment_sources": []map[string]any{
			{
				"repo_id":    "repo-deploy",
				"repo_name":  "sample-service-deploy",
				"confidence": 0.98,
				"reason":     "helm_values_reference",
			},
		},
		"k8s_resources": []map[string]any{
			{
				"entity_id":        "deployment-1",
				"entity_name":      "sample-service-api",
				"kind":             "Deployment",
				"relative_path":    "deploy/overlays/prod/deployment.yaml",
				"container_images": []string{"registry.example.test/sample-service-api:1.2.3"},
			},
		},
		"image_refs": []string{"registry.example.test/sample-service-api:1.2.3"},
		"controller_entities": []map[string]any{
			{
				"entity_id":       "argocd-app-1",
				"entity_name":     "sample-service-api",
				"controller_kind": "argocd_application",
				"relative_path":   "argocd/application.yaml",
				"source_root":     "deploy/overlays/prod",
				"source_roots":    []string{"deploy/overlays/prod"},
			},
		},
		"hostnames": []map[string]any{
			{"hostname": "sample-service-api.prod.example.test"},
			{"hostname": "sample-service-api.qa.example.test"},
		},
		"deployment_evidence": map[string]any{
			"delivery_paths": []map[string]any{
				{
					"kind":                      "workflow_artifact",
					"path":                      ".github/workflows/deploy.yaml",
					"artifact_type":             "github_actions_workflow",
					"workflow_name":             "deploy",
					"delivery_command_families": []string{"argocd", "helm"},
					"delivery_local_paths":      []string{"deploy/overlays/prod"},
				},
				{
					"kind":            "controller_artifact",
					"path":            "Jenkinsfile",
					"controller_kind": "jenkins_pipeline",
					"ansible_playbook_hints": []map[string]any{
						{"playbook": "deploy/overlays/prod/deployment.yaml"},
					},
				},
			},
		},
	}

	got := buildDeploymentTraceResponse("sample-service-api", ctx)

	provenanceOverview, ok := got["provenance_overview"].(map[string]any)
	if !ok {
		t.Fatalf("provenance_overview type = %T, want map[string]any", got["provenance_overview"])
	}
	if got, want := StringSliceVal(provenanceOverview, "families"), []string{"ansible", "argocd", "github_actions", "helm", "jenkins"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("provenance_overview.families = %#v, want %#v", got, want)
	}
	if got, want := IntVal(provenanceOverview, "artifact_lineage_count"), 3; got != want {
		t.Fatalf("provenance_overview.artifact_lineage_count = %d, want %d", got, want)
	}
	if got, want := IntVal(provenanceOverview, "workflow_artifact_count"), 1; got != want {
		t.Fatalf("provenance_overview.workflow_artifact_count = %d, want %d", got, want)
	}
	if got, want := IntVal(provenanceOverview, "controller_artifact_count"), 1; got != want {
		t.Fatalf("provenance_overview.controller_artifact_count = %d, want %d", got, want)
	}

	artifactLineage, ok := got["artifact_lineage"].([]map[string]any)
	if !ok {
		t.Fatalf("artifact_lineage type = %T, want []map[string]any", got["artifact_lineage"])
	}
	if len(artifactLineage) != 3 {
		t.Fatalf("len(artifact_lineage) = %d, want 3", len(artifactLineage))
	}

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	if got, want := StringSliceVal(deploymentOverview, "provenance_families"), []string{"ansible", "argocd", "github_actions", "helm", "jenkins"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("deployment_overview.provenance_families = %#v, want %#v", got, want)
	}
	if got, want := IntVal(deploymentOverview, "artifact_lineage_count"), 3; got != want {
		t.Fatalf("deployment_overview.artifact_lineage_count = %d, want %d", got, want)
	}

	story, ok := got["story"].(string)
	if !ok {
		t.Fatalf("story type = %T, want string", got["story"])
	}
	if !containsAllSubstrings(
		story,
		"Controller provenance",
		"Workflow provenance",
		".github/workflows/deploy.yaml",
		"Jenkinsfile",
	) {
		t.Fatalf("story = %q, want promoted workflow and controller provenance", story)
	}
}

func TestBuildDeploymentTraceResponseReportsProvenanceFamiliesWithoutArtifactLineage(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload-1",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repo-1",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{
			{
				"instance_id":   "inst-1",
				"platform_name": "modern",
				"platform_kind": "kubernetes",
				"environment":   "prod",
			},
		},
		"deployment_evidence": map[string]any{
			"delivery_paths": []map[string]any{
				{
					"kind":                      "workflow_artifact",
					"path":                      ".github/workflows/deploy.yaml",
					"artifact_type":             "github_actions_workflow",
					"workflow_name":             "deploy",
					"delivery_command_families": []string{"helm"},
				},
				{
					"kind":            "controller_artifact",
					"path":            "Jenkinsfile",
					"controller_kind": "jenkins_pipeline",
				},
			},
		},
	}

	got := buildDeploymentTraceResponse("sample-service-api", ctx)

	provenanceOverview, ok := got["provenance_overview"].(map[string]any)
	if !ok {
		t.Fatalf("provenance_overview type = %T, want map[string]any", got["provenance_overview"])
	}
	if got, want := StringSliceVal(provenanceOverview, "families"), []string{"github_actions", "helm", "jenkins"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("provenance_overview.families = %#v, want %#v", got, want)
	}

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	if got, want := StringSliceVal(deploymentOverview, "provenance_families"), []string{"github_actions", "helm", "jenkins"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("deployment_overview.provenance_families = %#v, want %#v", got, want)
	}
	if got, want := IntVal(provenanceOverview, "artifact_lineage_count"), 0; got != want {
		t.Fatalf("provenance_overview.artifact_lineage_count = %d, want %d", got, want)
	}
}
