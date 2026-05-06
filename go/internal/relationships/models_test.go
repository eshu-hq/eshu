package relationships

import "testing"

func TestEntityIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		entityID string
		repoID   string
		want     string
	}{
		{
			name:     "entity ID takes precedence",
			entityID: "platform:aws:eks:us-east-1:prod",
			repoID:   "repo-123",
			want:     "platform:aws:eks:us-east-1:prod",
		},
		{
			name:     "falls back to repo ID",
			entityID: "",
			repoID:   "repo-456",
			want:     "repo-456",
		},
		{
			name:     "both empty returns empty",
			entityID: "",
			repoID:   "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := entityIdentity(tt.entityID, tt.repoID)
			if got != tt.want {
				t.Errorf("entityIdentity(%q, %q) = %q, want %q",
					tt.entityID, tt.repoID, got, tt.want)
			}
		})
	}
}

func TestResolvedRelationshipIDIsDeterministic(t *testing.T) {
	t.Parallel()

	relationship := ResolvedRelationship{
		SourceEntityID:   "repo-service",
		TargetEntityID:   "repo-config",
		RelationshipType: RelDeploysFrom,
	}

	got := ResolvedRelationshipID("gen-1", relationship, 3)
	if got == "" {
		t.Fatal("ResolvedRelationshipID() = empty, want stable id")
	}
	if again := ResolvedRelationshipID("gen-1", relationship, 3); again != got {
		t.Fatalf("ResolvedRelationshipID() = %q then %q, want deterministic value", got, again)
	}
	if changed := ResolvedRelationshipID("gen-2", relationship, 3); changed == got {
		t.Fatalf("ResolvedRelationshipID() did not include generation: %q", changed)
	}
	if changed := ResolvedRelationshipID("gen-1", relationship, 4); changed == got {
		t.Fatalf("ResolvedRelationshipID() did not include ordinal: %q", changed)
	}
}

func TestEvidenceKindConstants(t *testing.T) {
	t.Parallel()

	// Verify key constants have expected string values.
	checks := map[EvidenceKind]string{
		EvidenceKindTerraformAppRepo:                     "TERRAFORM_APP_REPO",
		EvidenceKindTerraformModuleSource:                "TERRAFORM_MODULE_SOURCE",
		EvidenceKindTerragruntDependencyConfigPath:       "TERRAGRUNT_DEPENDENCY_CONFIG_PATH",
		EvidenceKindHelmChart:                            "HELM_CHART_REFERENCE",
		EvidenceKindArgoCDAppSource:                      "ARGOCD_APPLICATION_SOURCE",
		EvidenceKindJenkinsSharedLibrary:                 "JENKINS_SHARED_LIBRARY",
		EvidenceKindJenkinsGitHubRepository:              "JENKINS_GITHUB_REPOSITORY",
		EvidenceKindGitHubActionsWorkflowInputRepository: "GITHUB_ACTIONS_WORKFLOW_INPUT_REPOSITORY",
		EvidenceKindDockerComposeDependsOn:               "DOCKER_COMPOSE_DEPENDS_ON",
		EvidenceKindDockerfileSourceLabel:                "DOCKERFILE_SOURCE_LABEL",
		EvidenceKindKustomizeResource:                    "KUSTOMIZE_RESOURCE_REFERENCE",
		EvidenceKindKustomizeHelmChart:                   "KUSTOMIZE_HELM_CHART_REFERENCE",
		EvidenceKindKustomizeImage:                       "KUSTOMIZE_IMAGE_REFERENCE",
	}
	for kind, want := range checks {
		if string(kind) != want {
			t.Errorf("EvidenceKind %v = %q, want %q", kind, string(kind), want)
		}
	}
}

func TestRelationshipTypeConstants(t *testing.T) {
	t.Parallel()

	if string(RelDeploysFrom) != "DEPLOYS_FROM" {
		t.Errorf("RelDeploysFrom = %q", string(RelDeploysFrom))
	}
	if string(RelProvisionsDependencyFor) != "PROVISIONS_DEPENDENCY_FOR" {
		t.Errorf("RelProvisionsDependencyFor = %q", string(RelProvisionsDependencyFor))
	}
	if string(RelDependsOn) != "DEPENDS_ON" {
		t.Errorf("RelDependsOn = %q", string(RelDependsOn))
	}
}

func TestResolverOwnedRelationshipVocabulary(t *testing.T) {
	t.Parallel()

	got := []RelationshipType{
		RelDeploysFrom,
		RelDiscoversConfigIn,
		RelRunsOn,
		RelProvisionsDependencyFor,
		RelDependsOn,
		RelUsesModule,
	}
	want := map[RelationshipType]struct{}{
		RelDeploysFrom:             {},
		RelDiscoversConfigIn:       {},
		RelRunsOn:                  {},
		RelProvisionsDependencyFor: {},
		RelDependsOn:               {},
		RelUsesModule:              {},
	}

	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}

	seen := make(map[RelationshipType]struct{}, len(got))
	for _, rel := range got {
		seen[rel] = struct{}{}
	}

	for rel := range want {
		if _, ok := seen[rel]; !ok {
			t.Fatalf("missing resolver-owned relationship type %q", rel)
		}
	}

	for _, runtimeEdge := range []string{"PROVISIONS_PLATFORM", "DEFINES", "INSTANCE_OF", "DEPLOYMENT_SOURCE"} {
		for _, rel := range got {
			if string(rel) == runtimeEdge {
				t.Fatalf("runtime edge %q must not be part of the resolver-owned vocabulary", runtimeEdge)
			}
		}
	}
}

func TestResolutionSourceConstants(t *testing.T) {
	t.Parallel()

	if string(ResolutionSourceInferred) != "inferred" {
		t.Errorf("ResolutionSourceInferred = %q", string(ResolutionSourceInferred))
	}
	if string(ResolutionSourceAssertion) != "assertion" {
		t.Errorf("ResolutionSourceAssertion = %q", string(ResolutionSourceAssertion))
	}
}
