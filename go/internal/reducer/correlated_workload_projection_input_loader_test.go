package reducer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	correlationmodel "github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestCorrelatedWorkloadName(t *testing.T) {
	t.Parallel()

	candidate := WorkloadCandidate{
		RepoID:       "repo-sample-service",
		RepoName:     "sample-service-api",
		WorkloadName: "sample-service-api",
	}

	testCases := []struct {
		name      string
		evaluated correlationmodel.Candidate
		want      string
	}{
		{
			name: "uses admitted scoped unit key",
			evaluated: correlationmodel.Candidate{
				CorrelationKey: "repo-sample-service:sample-service-api",
			},
			want: "sample-service-api",
		},
		{
			name: "falls back for foreign correlation key",
			evaluated: correlationmodel.Candidate{
				CorrelationKey: "repo-other:remote",
			},
			want: "sample-service-api",
		},
		{
			name: "falls back for empty scoped suffix",
			evaluated: correlationmodel.Candidate{
				CorrelationKey: "repo-sample-service:",
			},
			want: "sample-service-api",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := correlatedWorkloadName(candidate, tc.evaluated); got != tc.want {
				t.Fatalf("correlatedWorkloadName(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCorrelatedWorkloadProjectionInputLoaderRejectsDockerfileOnlyCandidate(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
				{
					FactID:   "fact-repo-1",
					ScopeID:  "scope-service",
					FactKind: "repository",
					Payload: map[string]any{
						"graph_id": "repo-service",
						"name":     "service-repo",
					},
					ObservedAt: now,
				},
				{
					FactID:   "fact-file-1",
					ScopeID:  "scope-service",
					FactKind: "file",
					Payload: map[string]any{
						"repo_id":       "repo-service",
						"language":      "dockerfile",
						"relative_path": "Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
						},
					},
					ObservedAt: now,
				},
			},
		},
	}

	intent := Intent{
		IntentID:        "intent-correlation-1",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-service"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	candidates, _, err := loader.LoadWorkloadProjectionInputs(context.Background(), intent)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("len(candidates) = %d, want 0 for Dockerfile-only evidence", len(candidates))
	}
}

func TestCorrelatedWorkloadProjectionInputLoaderAdmitsResolvedDeploymentEvidence(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
				{
					FactID:   "fact-repo-1",
					ScopeID:  "scope-service",
					FactKind: "repository",
					Payload: map[string]any{
						"graph_id": "repo-service",
						"name":     "service-repo",
					},
					ObservedAt: now,
				},
				{
					FactID:   "fact-file-1",
					ScopeID:  "scope-service",
					FactKind: "file",
					Payload: map[string]any{
						"repo_id":       "repo-service",
						"language":      "dockerfile",
						"relative_path": "docker/api.Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
						},
					},
					ObservedAt: now,
				},
			},
		},
		ResolvedLoader: &stubResolvedRelationshipLoader{
			resolved: []relationships.ResolvedRelationship{
				{
					SourceRepoID:     "repo-delivery",
					TargetRepoID:     "repo-service",
					RelationshipType: relationships.RelDeploysFrom,
					Confidence:       0.96,
					Details: map[string]any{
						"evidence_kinds": []any{string(relationships.EvidenceKindKustomizeResource)},
					},
				},
			},
		},
	}

	intent := Intent{
		IntentID:        "intent-correlation-2",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-service"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	candidates, _, err := loader.LoadWorkloadProjectionInputs(context.Background(), intent)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if got, want := candidates[0].DeploymentRepoID, "repo-delivery"; got != want {
		t.Fatalf("DeploymentRepoID = %q, want %q", got, want)
	}
	if got, want := candidates[0].WorkloadName, "api"; got != want {
		t.Fatalf("WorkloadName = %q, want %q", got, want)
	}
	if got, want := candidates[0].Confidence, 0.96; got != want {
		t.Fatalf("Confidence = %v, want %v", got, want)
	}
}

func TestCorrelatedWorkloadProjectionInputLoaderCollapsesDockerSupportVariantsToRepoService(
	t *testing.T,
) {
	t.Parallel()

	now := time.Now().UTC()
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
				{
					FactID:   "fact-repo-1",
					ScopeID:  "scope-service",
					FactKind: "repository",
					Payload: map[string]any{
						"graph_id": "repo-sample-service",
						"name":     "sample-service-api",
					},
					ObservedAt: now,
				},
				{
					FactID:   "fact-file-remote",
					ScopeID:  "scope-service",
					FactKind: "file",
					Payload: map[string]any{
						"repo_id":       "repo-sample-service",
						"language":      "dockerfile",
						"relative_path": "docker/remote/Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
						},
					},
					ObservedAt: now,
				},
				{
					FactID:   "fact-file-local",
					ScopeID:  "scope-service",
					FactKind: "file",
					Payload: map[string]any{
						"repo_id":       "repo-sample-service",
						"language":      "dockerfile",
						"relative_path": "docker/local/Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
						},
					},
					ObservedAt: now,
				},
				{
					FactID:   "fact-file-jenkins",
					ScopeID:  "scope-service",
					FactKind: "file",
					Payload: map[string]any{
						"repo_id":       "repo-sample-service",
						"language":      "groovy",
						"relative_path": "Jenkinsfile",
						"parsed_file_data": map[string]any{
							"jenkins_pipeline_calls": []any{"pipelinePM2"},
						},
					},
					ObservedAt: now,
				},
			},
		},
	}

	intent := Intent{
		IntentID:        "intent-correlation-sample-service",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-sample-service"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	candidates, _, err := loader.LoadWorkloadProjectionInputs(context.Background(), intent)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v", err)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got, want := candidates[0].WorkloadName, "sample-service-api"; got != want {
		t.Fatalf("WorkloadName = %q, want %q", got, want)
	}
}

func TestCorrelatedWorkloadProjectionInputLoaderEnrichesDeploymentRepoEnvironments(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	// Source repo: has a K8s Deployment (passes admission at 0.98 confidence)
	// but no overlay directories → no environments from source facts.
	sourceEnvelopes := []facts.Envelope{
		{
			FactID: "fact-repo", ScopeID: "scope-app", FactKind: "repository",
			Payload:    map[string]any{"graph_id": "repo-app", "name": "my-service"},
			ObservedAt: now,
		},
		{
			FactID: "fact-file-k8s", ScopeID: "scope-app", FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-app", "language": "yaml",
				"relative_path": "k8s/deployment.yaml",
				"parsed_file_data": map[string]any{
					"k8s_resources": []any{
						map[string]any{"kind": "Deployment", "namespace": ""},
					},
				},
			},
			ObservedAt: now,
		},
	}

	// Deployment repo: has overlay environments in overlays/ directories.
	deployEnvelopes := []facts.Envelope{
		{
			FactID: "fact-deploy-repo", ScopeID: "scope-deploy", FactKind: "repository",
			Payload:    map[string]any{"graph_id": "repo-deploy", "name": "helm-charts"},
			ObservedAt: now,
		},
		{
			FactID: "fact-deploy-overlay-qa", ScopeID: "scope-deploy", FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-deploy", "language": "yaml",
				"relative_path":    "apps/my-service/overlays/qa/kustomization.yaml",
				"parsed_file_data": map[string]any{},
			},
			ObservedAt: now,
		},
		{
			FactID: "fact-deploy-overlay-prod", ScopeID: "scope-deploy", FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-deploy", "language": "yaml",
				"relative_path":    "apps/my-service/overlays/production/kustomization.yaml",
				"parsed_file_data": map[string]any{},
			},
			ObservedAt: now,
		},
	}

	factLoader := &scopedFactLoader{
		envelopesByScope: map[string][]facts.Envelope{
			"scope-app":    sourceEnvelopes,
			"scope-deploy": deployEnvelopes,
		},
	}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader: factLoader,
		ResolvedLoader: &stubResolvedRelationshipLoader{
			resolved: []relationships.ResolvedRelationship{
				{
					SourceRepoID:     "repo-deploy",
					TargetRepoID:     "repo-app",
					RelationshipType: relationships.RelDeploysFrom,
					Confidence:       0.96,
					Details: map[string]any{
						"evidence_kinds": []any{string(relationships.EvidenceKindKustomizeResource)},
					},
				},
			},
		},
		ScopeResolver: &stubScopeResolver{
			generations: map[string]RepoScopeIdentity{
				"repo-deploy": {ScopeID: "scope-deploy", GenerationID: "gen-deploy-1"},
			},
		},
	}

	intent := Intent{
		IntentID:        "intent-cross-repo-env",
		ScopeID:         "scope-app",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "replay after cross-repo resolution",
		EntityKeys:      []string{"repo-app"},
		RelatedScopeIDs: []string{"scope-app"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	candidates, deploymentEnvs, err := loader.LoadWorkloadProjectionInputs(context.Background(), intent)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if got, want := candidates[0].DeploymentRepoID, "repo-deploy"; got != want {
		t.Fatalf("DeploymentRepoID = %q, want %q", got, want)
	}

	// Critical assertion: deployment repo overlay environments must be populated.
	envs := deploymentEnvs["repo-deploy"]
	if len(envs) != 2 {
		t.Fatalf("deploymentEnvs[repo-deploy] = %v, want 2 environments (production, qa)", envs)
	}
	if envs[0] != "production" || envs[1] != "qa" {
		t.Fatalf("deploymentEnvs[repo-deploy] = %v, want [production qa]", envs)
	}
	if got, want := factLoader.kindCalls[0].scopeID, "scope-app"; got != want {
		t.Fatalf("kindCalls[0].scopeID = %q, want %q", got, want)
	}
	if got, want := strings.Join(factLoader.kindCalls[0].kinds, ","), "repository,file"; got != want {
		t.Fatalf("kindCalls[0].kinds = %q, want %q", got, want)
	}
	if got, want := factLoader.kindCalls[1].scopeID, "scope-deploy"; got != want {
		t.Fatalf("kindCalls[1].scopeID = %q, want %q", got, want)
	}
	if got, want := strings.Join(factLoader.kindCalls[1].kinds, ","), "file"; got != want {
		t.Fatalf("kindCalls[1].kinds = %q, want %q", got, want)
	}
}

func TestCorrelatedWorkloadProjectionInputLoaderEnrichesProvisioningRepoEnvironments(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	platformEvidenceKind := TerraformPlatformEvidenceKind("ecs", "cluster")

	sourceEnvelopes := []facts.Envelope{
		{
			FactID:     "fact-repo",
			ScopeID:    "scope-app",
			FactKind:   "repository",
			Payload:    map[string]any{"graph_id": "repo-app", "name": "my-service"},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-runtime",
			ScopeID:  "scope-app",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-app",
				"language":      "dockerfile",
				"relative_path": "Dockerfile",
				"parsed_file_data": map[string]any{
					"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
				},
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-workflow",
			ScopeID:  "scope-app",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-app",
				"language":      "yaml",
				"relative_path": ".github/workflows/build.yaml",
				"parsed_file_data": map[string]any{
					"artifact_type": "github_actions_workflow",
				},
			},
			ObservedAt: now,
		},
	}
	infraEnvelopes := []facts.Envelope{
		{
			FactID:     "fact-infra-repo",
			ScopeID:    "scope-infra",
			FactKind:   "repository",
			Payload:    map[string]any{"graph_id": "repo-infra", "name": "runtime-infra"},
			ObservedAt: now,
		},
		{
			FactID:   "fact-infra-env",
			ScopeID:  "scope-infra",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-infra",
				"language":         "hcl",
				"relative_path":    "environments/prod/terraform.tfvars",
				"parsed_file_data": map[string]any{},
			},
			ObservedAt: now,
		},
	}

	factLoader := &scopedFactLoader{
		envelopesByScope: map[string][]facts.Envelope{
			"scope-app":   sourceEnvelopes,
			"scope-infra": infraEnvelopes,
		},
	}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader: factLoader,
		ResolvedLoader: &stubResolvedRelationshipLoader{
			resolved: []relationships.ResolvedRelationship{
				{
					SourceRepoID:     "repo-deploy",
					TargetRepoID:     "repo-app",
					RelationshipType: relationships.RelDeploysFrom,
					Confidence:       0.96,
					Details: map[string]any{
						"evidence_kinds": []any{string(relationships.EvidenceKindKustomizeResource)},
					},
				},
				{
					SourceRepoID:     "repo-infra",
					TargetRepoID:     "repo-app",
					RelationshipType: relationships.RelProvisionsDependencyFor,
					Confidence:       0.94,
					Details: map[string]any{
						"evidence_kinds": []any{platformEvidenceKind},
					},
				},
			},
		},
		ScopeResolver: &stubScopeResolver{
			generations: map[string]RepoScopeIdentity{
				"repo-infra": {ScopeID: "scope-infra", GenerationID: "gen-infra-1"},
			},
		},
	}

	intent := Intent{
		IntentID:        "intent-provisioning-env",
		ScopeID:         "scope-app",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-app"},
		RelatedScopeIDs: []string{"scope-app"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	candidates, deploymentEnvs, err := loader.LoadWorkloadProjectionInputs(context.Background(), intent)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if got, want := candidates[0].ProvisioningRepoIDs, []string{"repo-infra"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ProvisioningRepoIDs = %v, want %v", got, want)
	}
	if got := candidates[0].ProvisioningEvidenceKinds["repo-infra"]; len(got) != 1 || got[0] != platformEvidenceKind {
		t.Fatalf("ProvisioningEvidenceKinds[repo-infra] = %v, want [%s]", got, platformEvidenceKind)
	}
	if got, want := deploymentEnvs["repo-infra"], []string{"prod"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("deploymentEnvs[repo-infra] = %v, want %v", got, want)
	}
}

func TestCorrelatedWorkloadProjectionInputLoaderUsesRepoScopedIncomingProvisioning(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	platformEvidenceKind := TerraformPlatformEvidenceKind("ecs", "service")

	sourceEnvelopes := []facts.Envelope{
		{
			FactID:     "fact-repo",
			ScopeID:    "scope-app",
			FactKind:   "repository",
			Payload:    map[string]any{"graph_id": "repo-app", "name": "my-service"},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-runtime",
			ScopeID:  "scope-app",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-app",
				"language":      "dockerfile",
				"relative_path": "Dockerfile",
				"parsed_file_data": map[string]any{
					"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
				},
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-workflow",
			ScopeID:  "scope-app",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-app",
				"language":      "yaml",
				"relative_path": ".github/workflows/build.yaml",
				"parsed_file_data": map[string]any{
					"artifact_type": "github_actions_workflow",
				},
			},
			ObservedAt: now,
		},
	}
	infraEnvelopes := []facts.Envelope{
		{
			FactID:   "fact-infra-env",
			ScopeID:  "scope-infra",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-infra",
				"relative_path": "environments/prod/terraform.tfvars",
			},
			ObservedAt: now,
		},
	}

	relationshipLoader := &stubResolvedRelationshipLoader{
		repoResolved: []relationships.ResolvedRelationship{
			{
				SourceRepoID:     "repo-infra",
				TargetRepoID:     "repo-app",
				RelationshipType: relationships.RelProvisionsDependencyFor,
				Confidence:       0.94,
				Details: map[string]any{
					"evidence_kinds": []any{platformEvidenceKind},
				},
			},
		},
	}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader: &scopedFactLoader{
			envelopesByScope: map[string][]facts.Envelope{
				"scope-app":   sourceEnvelopes,
				"scope-infra": infraEnvelopes,
			},
		},
		ResolvedLoader: relationshipLoader,
		ScopeResolver: &stubScopeResolver{
			generations: map[string]RepoScopeIdentity{
				"repo-infra": {ScopeID: "scope-infra", GenerationID: "gen-infra-1"},
			},
		},
	}

	intent := Intent{
		IntentID:        "intent-incoming-provisioning",
		ScopeID:         "scope-app",
		GenerationID:    "gen-app-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-app"},
		RelatedScopeIDs: []string{"scope-app"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	candidates, deploymentEnvs, err := loader.LoadWorkloadProjectionInputs(context.Background(), intent)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v", err)
	}
	if got, want := relationshipLoader.repoIDs, []string{"repo-app"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("repo-scoped relationship lookup repoIDs = %v, want %v", got, want)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if got, want := candidates[0].ProvisioningRepoIDs, []string{"repo-infra"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ProvisioningRepoIDs = %v, want %v", got, want)
	}
	if got := candidates[0].ProvisioningEvidenceKinds["repo-infra"]; len(got) != 1 || got[0] != platformEvidenceKind {
		t.Fatalf("ProvisioningEvidenceKinds[repo-infra] = %v, want [%s]", got, platformEvidenceKind)
	}
	if got, want := deploymentEnvs["repo-infra"], []string{"prod"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("deploymentEnvs[repo-infra] = %v, want %v", got, want)
	}
}

func TestCorrelatedWorkloadProjectionInputLoaderSkipsCrossRepoWhenResolverNil(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	// Source repo with K8s signal but no overlays. Deployment repo linked via
	// resolved relationship, but ScopeResolver is nil → no cross-repo env loading.
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
				{
					FactID: "fact-repo", ScopeID: "scope-app", FactKind: "repository",
					Payload:    map[string]any{"graph_id": "repo-app", "name": "my-service"},
					ObservedAt: now,
				},
				{
					FactID: "fact-file-k8s", ScopeID: "scope-app", FactKind: "file",
					Payload: map[string]any{
						"repo_id": "repo-app", "language": "yaml",
						"relative_path": "k8s/deployment.yaml",
						"parsed_file_data": map[string]any{
							"k8s_resources": []any{
								map[string]any{"kind": "Deployment", "namespace": ""},
							},
						},
					},
					ObservedAt: now,
				},
			},
		},
		ResolvedLoader: &stubResolvedRelationshipLoader{
			resolved: []relationships.ResolvedRelationship{
				{
					SourceRepoID:     "repo-deploy",
					TargetRepoID:     "repo-app",
					RelationshipType: relationships.RelDeploysFrom,
					Confidence:       0.96,
					Details: map[string]any{
						"evidence_kinds": []any{string(relationships.EvidenceKindKustomizeResource)},
					},
				},
			},
		},
		// ScopeResolver intentionally nil.
	}

	intent := Intent{
		IntentID:        "intent-no-resolver",
		ScopeID:         "scope-app",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-app"},
		RelatedScopeIDs: []string{"scope-app"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	candidates, deploymentEnvs, err := loader.LoadWorkloadProjectionInputs(context.Background(), intent)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}

	// Without ScopeResolver, deployment repo envs should be empty.
	if envs := deploymentEnvs["repo-deploy"]; len(envs) != 0 {
		t.Fatalf("deploymentEnvs[repo-deploy] = %v, want empty (no ScopeResolver)", envs)
	}
}

func TestCorrelatedWorkloadProjectionInputLoaderSkipsSameRepoDeployment(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	// Source repo that also has its own overlays. DeploymentRepoID == RepoID.
	// Environments should come from source facts, not trigger a cross-repo load.
	sourceEnvelopes := []facts.Envelope{
		{
			FactID: "fact-repo", ScopeID: "scope-app", FactKind: "repository",
			Payload:    map[string]any{"graph_id": "repo-self-deploy", "name": "self-deploy-svc"},
			ObservedAt: now,
		},
		{
			FactID: "fact-file-k8s", ScopeID: "scope-app", FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-self-deploy", "language": "yaml",
				"relative_path": "k8s/deployment.yaml",
				"parsed_file_data": map[string]any{
					"k8s_resources": []any{
						map[string]any{"kind": "Deployment", "namespace": ""},
					},
				},
			},
			ObservedAt: now,
		},
		{
			FactID: "fact-file-overlay", ScopeID: "scope-app", FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-self-deploy", "language": "yaml",
				"relative_path":    "overlays/staging/kustomization.yaml",
				"parsed_file_data": map[string]any{},
			},
			ObservedAt: now,
		},
	}

	resolver := &stubScopeResolver{calls: 0}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:    &stubFactLoader{envelopes: sourceEnvelopes},
		ScopeResolver: resolver,
	}

	intent := Intent{
		IntentID:        "intent-self-deploy",
		ScopeID:         "scope-app",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-self-deploy"},
		RelatedScopeIDs: []string{"scope-app"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	candidates, deploymentEnvs, err := loader.LoadWorkloadProjectionInputs(context.Background(), intent)
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}

	// Source repo's own overlays should be present.
	if envs := deploymentEnvs["repo-self-deploy"]; len(envs) != 1 || envs[0] != "staging" {
		t.Fatalf("deploymentEnvs[repo-self-deploy] = %v, want [staging]", envs)
	}

	// ScopeResolver should NOT be called for same-repo deployment.
	if resolver.calls != 0 {
		t.Fatalf("ScopeResolver.calls = %d, want 0 (no cross-repo lookup for same repo)", resolver.calls)
	}
}

// -- test fakes for cross-repo loading --

type scopedFactLoader struct {
	envelopesByScope map[string][]facts.Envelope
	calls            int
	kindCalls        []scopedFactKindCall
}

type scopedFactKindCall struct {
	scopeID string
	kinds   []string
}

func (f *scopedFactLoader) ListFacts(_ context.Context, scopeID, _ string) ([]facts.Envelope, error) {
	f.calls++
	envelopes, ok := f.envelopesByScope[scopeID]
	if !ok {
		return nil, fmt.Errorf("no envelopes for scope %q", scopeID)
	}
	return envelopes, nil
}

func (f *scopedFactLoader) ListFactsByKind(
	_ context.Context,
	scopeID string,
	_ string,
	factKinds []string,
) ([]facts.Envelope, error) {
	f.kindCalls = append(f.kindCalls, scopedFactKindCall{
		scopeID: scopeID,
		kinds:   append([]string(nil), factKinds...),
	})
	envelopes, ok := f.envelopesByScope[scopeID]
	if !ok {
		return nil, fmt.Errorf("no envelopes for scope %q", scopeID)
	}
	allowed := make(map[string]struct{}, len(factKinds))
	for _, factKind := range factKinds {
		allowed[factKind] = struct{}{}
	}
	filtered := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if _, ok := allowed[envelope.FactKind]; ok {
			filtered = append(filtered, envelope)
		}
	}
	return filtered, nil
}

type stubScopeResolver struct {
	generations map[string]RepoScopeIdentity
	calls       int
}

func (r *stubScopeResolver) ResolveRepoActiveGenerations(
	_ context.Context,
	repoIDs []string,
) (map[string]RepoScopeIdentity, error) {
	r.calls++
	result := make(map[string]RepoScopeIdentity)
	for _, id := range repoIDs {
		if identity, ok := r.generations[id]; ok {
			result[id] = identity
		}
	}
	return result, nil
}
