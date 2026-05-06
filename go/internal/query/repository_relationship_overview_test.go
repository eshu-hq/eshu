package query

import (
	"strings"
	"testing"
)

func TestBuildRepositoryRelationshipOverviewSeparatesControllerAndWorkflowEvidence(t *testing.T) {
	t.Parallel()

	overview := buildRepositoryRelationshipOverview([]map[string]any{
		{
			"type":          "DEPLOYS_FROM",
			"target_name":   "infra-configs",
			"target_id":     "repo-2",
			"evidence_type": "argocd_application_source",
		},
		{
			"type":          "DEPLOYS_FROM",
			"target_name":   "ci-workflows",
			"target_id":     "repo-3",
			"evidence_type": "github_actions_reusable_workflow_ref",
		},
		{
			"type":          "DEPENDS_ON",
			"target_name":   "terraform-modules",
			"target_id":     "repo-4",
			"evidence_type": "terraform_module_source",
		},
		{
			"type":          "DEPLOYS_FROM",
			"target_name":   "payments-service",
			"target_id":     "repo-7",
			"evidence_type": "dockerfile_source_label",
		},
		{
			"type":          "DISCOVERS_CONFIG_IN",
			"target_name":   "shared-pipelines",
			"target_id":     "repo-5",
			"evidence_type": "jenkins_shared_library",
		},
		{
			"type":          "DEPENDS_ON",
			"target_name":   "ansible-ops",
			"target_id":     "repo-6",
			"evidence_type": "ansible_role_reference",
		},
	})

	if overview == nil {
		t.Fatal("buildRepositoryRelationshipOverview() = nil, want typed relationship overview")
	}

	if got, want := overview["relationship_count"], 6; got != want {
		t.Fatalf("relationship_count = %#v, want %#v", got, want)
	}

	evidenceTypes, ok := overview["evidence_types"].([]string)
	if !ok {
		t.Fatalf("evidence_types type = %T, want []string", overview["evidence_types"])
	}
	if len(evidenceTypes) != 6 {
		t.Fatalf("len(evidence_types) = %d, want 6", len(evidenceTypes))
	}

	controllerDriven, ok := overview["controller_driven"].([]map[string]any)
	if !ok {
		t.Fatalf("controller_driven type = %T, want []map[string]any", overview["controller_driven"])
	}
	if len(controllerDriven) != 3 {
		t.Fatalf("len(controller_driven) = %d, want 3", len(controllerDriven))
	}
	controllerEvidence := map[string]struct{}{}
	for _, row := range controllerDriven {
		controllerEvidence[StringVal(row, "evidence_type")] = struct{}{}
	}
	for _, want := range []string{"argocd_application_source", "ansible_role_reference", "jenkins_shared_library"} {
		if _, ok := controllerEvidence[want]; !ok {
			t.Fatalf("controller_driven missing evidence_type %q", want)
		}
	}

	workflowDriven, ok := overview["workflow_driven"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow_driven type = %T, want []map[string]any", overview["workflow_driven"])
	}
	if len(workflowDriven) != 1 {
		t.Fatalf("len(workflow_driven) = %d, want 1", len(workflowDriven))
	}
	if got, want := workflowDriven[0]["evidence_type"], "github_actions_reusable_workflow_ref"; got != want {
		t.Fatalf("workflow_driven[0].evidence_type = %#v, want %#v", got, want)
	}

	iacDriven, ok := overview["iac_driven"].([]map[string]any)
	if !ok {
		t.Fatalf("iac_driven type = %T, want []map[string]any", overview["iac_driven"])
	}
	if len(iacDriven) != 2 {
		t.Fatalf("len(iac_driven) = %d, want 2", len(iacDriven))
	}

	story, ok := overview["story"].(string)
	if !ok {
		t.Fatalf("story type = %T, want string", overview["story"])
	}
	if story == "" {
		t.Fatal("story is empty, want typed relationship narrative")
	}
	if !containsSubstring(story, "dockerfile_source_label") {
		t.Fatalf("story = %q, want dockerfile_source_label evidence summary", story)
	}
}

func TestBuildRepositoryRelationshipOverviewTreatsDockerComposeAsIACDriven(t *testing.T) {
	t.Parallel()

	overview := buildRepositoryRelationshipOverview([]map[string]any{
		{
			"type":          "DEPLOYS_FROM",
			"target_name":   "service-worker-jobs",
			"target_id":     "repo-worker",
			"evidence_type": "docker_compose_build_context",
		},
		{
			"type":          "DEPLOYS_FROM",
			"target_name":   "service-worker-jobs",
			"target_id":     "repo-worker",
			"evidence_type": "docker_compose_image",
		},
		{
			"type":          "DEPENDS_ON",
			"target_name":   "service-worker-jobs",
			"target_id":     "repo-worker",
			"evidence_type": "docker_compose_depends_on",
		},
	})

	if overview == nil {
		t.Fatal("buildRepositoryRelationshipOverview() = nil, want typed relationship overview")
	}

	iacDriven, ok := overview["iac_driven"].([]map[string]any)
	if !ok {
		t.Fatalf("iac_driven type = %T, want []map[string]any", overview["iac_driven"])
	}
	if len(iacDriven) != 3 {
		t.Fatalf("len(iac_driven) = %d, want 3", len(iacDriven))
	}

	evidenceTypes := map[string]struct{}{}
	for _, row := range iacDriven {
		evidenceTypes[StringVal(row, "evidence_type")] = struct{}{}
	}
	for _, want := range []string{"docker_compose_build_context", "docker_compose_image", "docker_compose_depends_on"} {
		if _, ok := evidenceTypes[want]; !ok {
			t.Fatalf("iac_driven missing evidence_type %q", want)
		}
	}
}

func containsSubstring(value string, needle string) bool {
	return strings.Contains(value, needle)
}
