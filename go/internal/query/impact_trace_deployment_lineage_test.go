package query

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildDeploymentTraceArtifactLineageUsesExplicitWorkflowAndControllerEvidence(t *testing.T) {
	t.Parallel()

	controllerEntities := []map[string]any{
		{
			"entity_id":       "argocd-app-1",
			"entity_name":     "sample-service-api",
			"controller_kind": "argocd_application",
			"relative_path":   "argocd/application.yaml",
			"source_root":     "deploy/overlays/prod",
			"source_roots":    []string{"deploy/overlays/prod"},
		},
	}
	deploymentEvidence := map[string]any{
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
				"ansible_inventories": []string{"inventory/prod.ini"},
				"ansible_var_files":   []string{"group_vars/all.yml"},
				"ansible_task_entrypoints": []string{
					"roles/deploy/tasks/main.yml",
				},
			},
		},
	}
	k8sResources := []map[string]any{
		{
			"entity_id":        "service-1",
			"entity_name":      "sample-service-api",
			"kind":             "Service",
			"relative_path":    "deploy/overlays/prod/service.yaml",
			"container_images": []string{},
		},
		{
			"entity_id":     "deployment-1",
			"entity_name":   "sample-service-api",
			"kind":          "Deployment",
			"relative_path": "deploy/overlays/prod/deployment.yaml",
			"container_images": []string{
				"registry.example.test/sample-service-api:1.2.3",
			},
		},
	}
	hostnames := []map[string]any{
		{"hostname": "sample-service-api.dev.example.test"},
		{"hostname": "sample-service-api.prod.example.test"},
		{"hostname": "sample-service-api.qa.example.test"},
		{"hostname": "sample-service-api.stage.example.test"},
		{"hostname": "sample-service-api.extra.example.test"},
	}

	got := buildDeploymentTraceArtifactLineage(
		controllerEntities,
		deploymentEvidence,
		k8sResources,
		hostnames,
		nil,
	)
	if len(got) != 3 {
		t.Fatalf("len(buildDeploymentTraceArtifactLineage()) = %d, want 3", len(got))
	}

	argocd := requireDeploymentTraceLineageRow(
		t,
		got,
		"controller_entity",
		"argocd/application.yaml",
	)
	if got, want := traceString(argocd, "artifact_kind"), "controller_source_root"; got != want {
		t.Fatalf("argocd.artifact_kind = %q, want %q", got, want)
	}
	if got, want := traceString(argocd, "artifact_value"), "deploy/overlays/prod"; got != want {
		t.Fatalf("argocd.artifact_value = %q, want %q", got, want)
	}
	if got, want := traceString(argocd, "deployment_target_kind"), "Deployment"; got != want {
		t.Fatalf("argocd.deployment_target_kind = %q, want %q", got, want)
	}

	workflow := requireDeploymentTraceLineageRow(
		t,
		got,
		"workflow_artifact",
		".github/workflows/deploy.yaml",
	)
	if got, want := traceStringSlice(workflow, "provenance_families"), []string{"argocd", "github_actions", "helm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workflow.provenance_families = %#v, want %#v", got, want)
	}
	if got, want := traceString(workflow, "artifact_kind"), "delivery_local_path"; got != want {
		t.Fatalf("workflow.artifact_kind = %q, want %q", got, want)
	}
	if got, want := traceString(workflow, "artifact_value"), "deploy/overlays/prod"; got != want {
		t.Fatalf("workflow.artifact_value = %q, want %q", got, want)
	}
	if got, want := traceStringSlice(workflow, "image_refs"), []string{"registry.example.test/sample-service-api:1.2.3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workflow.image_refs = %#v, want %#v", got, want)
	}
	if got, want := traceStringSlice(workflow, "service_entrypoints"), []string{
		"sample-service-api.dev.example.test",
		"sample-service-api.prod.example.test",
		"sample-service-api.qa.example.test",
		"sample-service-api.stage.example.test",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workflow.service_entrypoints = %#v, want %#v", got, want)
	}

	jenkins := requireDeploymentTraceLineageRow(
		t,
		got,
		"controller_artifact",
		"Jenkinsfile",
	)
	if got, want := traceStringSlice(jenkins, "provenance_families"), []string{"ansible", "jenkins"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("jenkins.provenance_families = %#v, want %#v", got, want)
	}
	if got, want := traceString(jenkins, "artifact_kind"), "ansible_playbook_hint"; got != want {
		t.Fatalf("jenkins.artifact_kind = %q, want %q", got, want)
	}
	if got, want := traceString(jenkins, "artifact_value"), "deploy/overlays/prod/deployment.yaml"; got != want {
		t.Fatalf("jenkins.artifact_value = %q, want %q", got, want)
	}
}

func TestBuildDeploymentTraceArtifactLineageSkipsImplicitOnlyEvidence(t *testing.T) {
	t.Parallel()

	deploymentEvidence := map[string]any{
		"delivery_paths": []map[string]any{
			{
				"kind":                      "workflow_artifact",
				"path":                      ".github/workflows/deploy.yaml",
				"artifact_type":             "github_actions_workflow",
				"delivery_command_families": []string{"helm"},
			},
			{
				"kind":            "controller_artifact",
				"path":            "Jenkinsfile",
				"controller_kind": "jenkins_pipeline",
			},
		},
	}
	k8sResources := []map[string]any{
		{
			"entity_id":     "deployment-1",
			"entity_name":   "sample-service-api",
			"kind":          "Deployment",
			"relative_path": "deploy/overlays/prod/deployment.yaml",
			"container_images": []string{
				"registry.example.test/sample-service-api:1.2.3",
			},
		},
	}

	got := buildDeploymentTraceArtifactLineage(nil, deploymentEvidence, k8sResources, nil, nil)
	if len(got) != 0 {
		t.Fatalf("buildDeploymentTraceArtifactLineage() = %#v, want no lineage without explicit artifact evidence", got)
	}
}

func requireDeploymentTraceLineageRow(
	t *testing.T,
	rows []map[string]any,
	sourceKind string,
	sourcePath string,
) map[string]any {
	t.Helper()

	for _, row := range rows {
		if traceString(row, "source_kind") == sourceKind && traceString(row, "source_path") == sourcePath {
			return row
		}
	}
	t.Fatalf("missing lineage row for source_kind=%q source_path=%q in %#v", sourceKind, sourcePath, rows)
	return nil
}

func containsAllSubstrings(value string, parts ...string) bool {
	for _, part := range parts {
		if !lineageTestContainsSubstring(value, part) {
			return false
		}
	}
	return true
}

func lineageTestContainsSubstring(value string, part string) bool {
	return strings.Contains(value, part)
}
