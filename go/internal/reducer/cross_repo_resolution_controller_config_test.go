package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestCrossRepoResolutionPreservesControllerAndConfigEvidenceFamilies(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		evidenceKind     relationships.EvidenceKind
		relationshipType relationships.RelationshipType
		wantEvidenceType string
	}{
		{
			name:             "github actions action repository stays depends_on",
			evidenceKind:     relationships.EvidenceKindGitHubActionsActionRepository,
			relationshipType: relationships.RelDependsOn,
			wantEvidenceType: "github_actions_action_repository",
		},
		{
			name:             "jenkins shared library stays discovers_config_in",
			evidenceKind:     relationships.EvidenceKindJenkinsSharedLibrary,
			relationshipType: relationships.RelDiscoversConfigIn,
			wantEvidenceType: "jenkins_shared_library",
		},
		{
			name:             "jenkins github repository stays discovers_config_in",
			evidenceKind:     relationships.EvidenceKindJenkinsGitHubRepository,
			relationshipType: relationships.RelDiscoversConfigIn,
			wantEvidenceType: "jenkins_github_repository",
		},
		{
			name:             "ansible role reference stays depends_on",
			evidenceKind:     relationships.EvidenceKindAnsibleRoleReference,
			relationshipType: relationships.RelDependsOn,
			wantEvidenceType: "ansible_role_reference",
		},
		{
			name:             "terragrunt dependency config path stays discovers_config_in",
			evidenceKind:     relationships.EvidenceKindTerragruntDependencyConfigPath,
			relationshipType: relationships.RelDiscoversConfigIn,
			wantEvidenceType: "terragrunt_dependency_config_path",
		},
		{
			name:             "terragrunt config asset stays discovers_config_in",
			evidenceKind:     relationships.EvidenceKindTerragruntConfigAssetPath,
			relationshipType: relationships.RelDiscoversConfigIn,
			wantEvidenceType: "terragrunt_config_asset_path",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			intentWriter := &recordingRepoDependencyIntentWriter{}
			handler := CrossRepoRelationshipHandler{
				EvidenceLoader: &fakeEvidenceFactLoader{facts: []relationships.EvidenceFact{
					{
						EvidenceKind:     tc.evidenceKind,
						RelationshipType: tc.relationshipType,
						SourceRepoID:     "repo-service",
						TargetRepoID:     "repo-target",
						Confidence:       0.92,
					},
				}},
				IntentWriter: intentWriter,
			}

			count, err := handler.Resolve(context.Background(), "scope-controller", "gen-controller")
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if count != 1 {
				t.Fatalf("Resolve() = %d, want 1", count)
			}
			if len(intentWriter.rows) != 1 {
				t.Fatalf("expected 1 intent write, got %d", len(intentWriter.rows))
			}
			if len(intentWriter.rows[0]) != 1 {
				t.Fatalf("expected 1 intent row, got %d", len(intentWriter.rows[0]))
			}

			row := intentWriter.rows[0][0]
			if got := stringValue(row.Payload["repo_id"]); got != "repo-service" {
				t.Fatalf("row repo_id = %q, want %q", got, "repo-service")
			}
			if got := stringValue(row.Payload["target_repo_id"]); got != "repo-target" {
				t.Fatalf("row target_repo_id = %q, want %q", got, "repo-target")
			}
			if got, want := stringValue(row.Payload["relationship_type"]), string(tc.relationshipType); got != want {
				t.Fatalf("row relationship_type = %q, want %q", got, want)
			}
			if got, want := stringValue(row.Payload["evidence_type"]), tc.wantEvidenceType; got != want {
				t.Fatalf("row evidence_type = %q, want %q", got, want)
			}
		})
	}
}

func TestCrossRepoResolutionPreservesLocalReusableWorkflowAsSameRepoDeploySource(t *testing.T) {
	t.Parallel()

	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindGitHubActionsLocalReusableWorkflow,
				RelationshipType: relationships.RelDeploysFrom,
				SourceRepoID:     "repo-service",
				TargetRepoID:     "repo-service",
				Confidence:       0.86,
			},
		}},
		IntentWriter: intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-workflow", "gen-workflow")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want 1", count)
	}
	if len(intentWriter.rows) != 1 || len(intentWriter.rows[0]) != 1 {
		t.Fatalf("intent writes = %#v, want 1 row", intentWriter.rows)
	}

	row := intentWriter.rows[0][0]
	if got, want := stringValue(row.Payload["repo_id"]), "repo-service"; got != want {
		t.Fatalf("row repo_id = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["target_repo_id"]), "repo-service"; got != want {
		t.Fatalf("row target_repo_id = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["relationship_type"]), string(relationships.RelDeploysFrom); got != want {
		t.Fatalf("row relationship_type = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["evidence_type"]), "github_actions_local_reusable_workflow_ref"; got != want {
		t.Fatalf("row evidence_type = %q, want %q", got, want)
	}
}
