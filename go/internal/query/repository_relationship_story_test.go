package query

import (
	"strings"
	"testing"
)

func TestBuildRepositoryStoryResponseIncludesTypedRelationshipNarrative(t *testing.T) {
	t.Parallel()

	repo := RepoRef{
		ID:        "repository:payments",
		Name:      "payments",
		LocalPath: "/workspace/payments",
		RemoteURL: "https://github.com/acme/payments.git",
		RepoSlug:  "acme/payments",
		HasRemote: true,
	}

	got := buildRepositoryStoryResponse(
		repo,
		42,
		[]string{"go", "yaml"},
		[]string{"payments-api"},
		[]string{"argocd_application"},
		4,
		map[string]any{
			"families": []string{"argocd", "ansible", "helm", "terraform"},
			"relationship_overview": map[string]any{
				"relationship_count": 4,
				"story": "Controller-driven relationships: DEPLOYS_FROM infra-configs via argocd_application_source. " +
					"Workflow-driven relationships: DEPLOYS_FROM ci-workflows via github_actions_reusable_workflow_ref. " +
					"Controller-driven relationships: DISCOVERS_CONFIG_IN controller-pipelines via jenkins_shared_library. " +
					"Controller-driven relationships: DEPENDS_ON ansible-ops via ansible_role_reference.",
				"controller_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "infra-configs",
						"target_id":     "repo-2",
						"evidence_type": "argocd_application_source",
					},
					{
						"type":          "DISCOVERS_CONFIG_IN",
						"target_name":   "controller-pipelines",
						"target_id":     "repo-3",
						"evidence_type": "jenkins_shared_library",
					},
					{
						"type":          "DEPENDS_ON",
						"target_name":   "ansible-ops",
						"target_id":     "repo-4",
						"evidence_type": "ansible_role_reference",
					},
				},
				"workflow_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "ci-workflows",
						"target_id":     "repo-5",
						"evidence_type": "github_actions_reusable_workflow_ref",
					},
				},
			},
		},
		nil,
	)

	relationshipOverview, ok := got["relationship_overview"].(map[string]any)
	if !ok {
		t.Fatalf("relationship_overview type = %T, want map[string]any", got["relationship_overview"])
	}

	if got, want := relationshipOverview["relationship_count"], 4; got != want {
		t.Fatalf("relationship_overview.relationship_count = %#v, want %#v", got, want)
	}

	controllerDriven, ok := relationshipOverview["controller_driven"].([]map[string]any)
	if !ok {
		t.Fatalf("relationship_overview.controller_driven type = %T, want []map[string]any", relationshipOverview["controller_driven"])
	}
	if len(controllerDriven) != 3 {
		t.Fatalf("len(relationship_overview.controller_driven) = %d, want 3", len(controllerDriven))
	}
	controllerEvidence := map[string]struct{}{}
	for _, row := range controllerDriven {
		controllerEvidence[row["evidence_type"].(string)] = struct{}{}
	}
	for _, want := range []string{"argocd_application_source", "jenkins_shared_library", "ansible_role_reference"} {
		if _, ok := controllerEvidence[want]; !ok {
			t.Fatalf("relationship_overview.controller_driven missing evidence_type %q", want)
		}
	}

	workflowDriven, ok := relationshipOverview["workflow_driven"].([]map[string]any)
	if !ok {
		t.Fatalf("relationship_overview.workflow_driven type = %T, want []map[string]any", relationshipOverview["workflow_driven"])
	}
	if len(workflowDriven) != 1 {
		t.Fatalf("len(relationship_overview.workflow_driven) = %d, want 1", len(workflowDriven))
	}
	if got, want := workflowDriven[0]["evidence_type"], "github_actions_reusable_workflow_ref"; got != want {
		t.Fatalf("relationship_overview.workflow_driven[0].evidence_type = %#v, want %#v", got, want)
	}

	story, ok := relationshipOverview["story"].(string)
	if !ok {
		t.Fatalf("relationship_overview.story type = %T, want string", relationshipOverview["story"])
	}
	if story == "" {
		t.Fatal("relationship_overview.story is empty, want typed relationship narrative")
	}
	lowerStory := strings.ToLower(story)
	for _, want := range []string{
		"controller-driven",
		"workflow-driven",
		"argocd_application_source",
		"jenkins_shared_library",
		"ansible_role_reference",
		"github_actions_reusable_workflow_ref",
	} {
		if !strings.Contains(lowerStory, strings.ToLower(want)) {
			t.Fatalf("relationship_overview.story = %q, want %q", story, want)
		}
	}

	storySections, ok := got["story_sections"].([]map[string]any)
	if !ok {
		t.Fatalf("story_sections type = %T, want []map[string]any", got["story_sections"])
	}
	found := false
	for _, section := range storySections {
		if section["title"] == "relationships" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("story_sections missing relationships section")
	}
}
