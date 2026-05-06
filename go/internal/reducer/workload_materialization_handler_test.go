package reducer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestWorkloadMaterializationHandlerMaterializesFromFacts(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-payments",
					"name":     "payments",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id": "repo-payments",
					"parsed_file_data": map[string]any{
						"k8s_resources": []any{
							map[string]any{
								"name":      "payments",
								"kind":      "Deployment",
								"namespace": "production",
							},
						},
					},
				},
				ObservedAt: now,
			},
		},
	}

	executor := &recordingCypherExecutor{}
	materializer := NewWorkloadMaterializer(executor)

	handler := WorkloadMaterializationHandler{
		FactLoader:   loader,
		Materializer: materializer,
	}

	intent := Intent{
		IntentID:        "intent-wm-1",
		ScopeID:         "scope-payments",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-payments"},
		RelatedScopeIDs: []string{"scope-payments"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want succeeded", result.Status)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0, want > 0")
	}
	if loader.calls != 1 {
		t.Fatalf("FactLoader.ListFacts calls = %d, want 1", loader.calls)
	}
	if len(executor.calls) == 0 {
		t.Fatal("CypherExecutor calls = 0, want > 0")
	}
}

func TestWorkloadMaterializationHandlerNoCandidatesSucceeds(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-docs",
					"name":     "docs",
				},
				ObservedAt: now,
			},
		},
	}

	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		Materializer:   NewWorkloadMaterializer(&recordingCypherExecutor{}),
		PhasePublisher: &recordingGraphProjectionPhasePublisher{},
	}

	intent := Intent{
		IntentID:        "intent-wm-2",
		ScopeID:         "scope-docs",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-docs"},
		RelatedScopeIDs: []string{"scope-docs"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want succeeded", result.Status)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 (no candidates)", result.CanonicalWrites)
	}
}

func TestWorkloadMaterializationHandlerPublishesPhaseWhenNoCandidates(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-docs",
					"name":     "docs",
				},
				ObservedAt: now,
			},
		},
	}
	publisher := &recordingGraphProjectionPhasePublisher{}

	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		Materializer:   NewWorkloadMaterializer(&recordingCypherExecutor{}),
		PhasePublisher: publisher,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-wm-phase-none",
		ScopeID:         "scope-docs",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-docs"},
		RelatedScopeIDs: []string{"scope-docs"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("publisher call count = %d, want %d", got, want)
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, GraphProjectionKeyspaceServiceUID; got != want {
		t.Fatalf("published keyspace = %q, want %q", got, want)
	}
	if got, want := publisher.calls[0][0].Phase, GraphProjectionPhaseWorkloadMaterialization; got != want {
		t.Fatalf("published phase = %q, want %q", got, want)
	}
}

func TestWorkloadMaterializationHandlerUsesCorrelatedInputsByDefault(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-service",
					"name":     "service-repo",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-service",
					"language":      "dockerfile",
					"relative_path": "Dockerfile",
					"parsed_file_data": map[string]any{
						"dockerfile_stages": []any{
							map[string]any{"name": "runtime"},
						},
					},
				},
				ObservedAt: now,
			},
		},
	}

	handler := WorkloadMaterializationHandler{
		FactLoader:   loader,
		Materializer: NewWorkloadMaterializer(&recordingCypherExecutor{}),
	}

	intent := Intent{
		IntentID:        "intent-wm-correlation-default",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-service"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want succeeded", result.Status)
	}
	if got, want := result.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d for low-confidence dockerfile-only evidence", got, want)
	}
}

func TestWorkloadMaterializationHandlerUsesArgoDeploymentSourceRelationships(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-service-edge-api",
					"name":     "service-edge-api",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id": "repo-service-edge-api",
					"parsed_file_data": map[string]any{
						"k8s_resources": []any{
							map[string]any{
								"name":      "service-edge-api",
								"kind":      "Deployment",
								"namespace": "production",
							},
						},
					},
				},
				ObservedAt: now,
			},
		},
	}
	relationshipLoader := &stubResolvedRelationshipLoader{
		resolved: []relationships.ResolvedRelationship{
			{
				SourceRepoID:     "repo-service-edge-api",
				TargetRepoID:     "repo-deployment-kustomize",
				RelationshipType: relationships.RelDeploysFrom,
				Details: map[string]any{
					"evidence_kinds": []any{
						string(relationships.EvidenceKindArgoCDApplicationSetDeploySource),
					},
				},
			},
		},
	}

	executor := &recordingCypherExecutor{}
	materializer := NewWorkloadMaterializer(executor)

	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		ResolvedLoader: relationshipLoader,
		Materializer:   materializer,
	}

	intent := Intent{
		IntentID:        "intent-wm-deploy-source",
		ScopeID:         "scope-service-edge-api",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-service-edge-api"},
		RelatedScopeIDs: []string{"scope-service-edge-api"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0, want > 0")
	}
	if relationshipLoader.calls != 1 {
		t.Fatalf("ResolvedLoader.GetResolvedRelationships calls = %d, want 1", relationshipLoader.calls)
	}
	if !containsRecordedCypher(executor.calls, "MERGE (i)-[rel:DEPLOYMENT_SOURCE]->(deployment_repo)") {
		t.Fatal("missing DEPLOYMENT_SOURCE MERGE cypher")
	}
	if !recordedCallContainsParam(executor.calls, "deployment_repo_id", "repo-deployment-kustomize") {
		t.Fatal("missing deployment_repo_id row for repo-deployment-kustomize")
	}
}

func TestWorkloadMaterializationHandlerSeedsRuntimeCandidateFromArgoDeploymentSource(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-edge-api",
					"name":     "edge-api",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-repo-deploy",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-platform-deploy",
					"name":     "platform-deploy",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-edge-api",
					"language":      "dockerfile",
					"relative_path": "Dockerfile",
					"parsed_file_data": map[string]any{
						"dockerfile_stages": []any{
							map[string]any{"name": "runtime"},
						},
					},
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file-deploy",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-platform-deploy",
					"language":      "yaml",
					"relative_path": "overlays/production/application.yaml",
					"parsed_file_data": map[string]any{
						"argocd_applications": []any{
							map[string]any{"name": "edge-api"},
						},
					},
				},
				ObservedAt: now,
			},
		},
	}
	relationshipLoader := &stubResolvedRelationshipLoader{
		resolved: []relationships.ResolvedRelationship{
			{
				SourceRepoID:     "repo-edge-api",
				TargetRepoID:     "repo-platform-deploy",
				RelationshipType: relationships.RelDeploysFrom,
				Confidence:       0.96,
				Details: map[string]any{
					"evidence_kinds": []any{
						string(relationships.EvidenceKindArgoCDAppSource),
					},
				},
			},
		},
	}

	executor := &recordingCypherExecutor{}
	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		ResolvedLoader: relationshipLoader,
		Materializer:   NewWorkloadMaterializer(executor),
	}

	candidates, deploymentEnvironments := ExtractWorkloadCandidates(loader.envelopes)
	if got, want := deploymentEnvironments["repo-platform-deploy"], []string{"production"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("deploymentEnvironments[repo-platform-deploy] = %v, want %v", got, want)
	}
	candidates = applyResolvedDeploymentSources(candidates, relationshipLoader.resolved)
	if got, want := candidates[0].DeploymentRepoID, "repo-platform-deploy"; got != want {
		t.Fatalf("candidates[0].DeploymentRepoID = %q, want %q", got, want)
	}
	projection := BuildProjectionRows(candidates, deploymentEnvironments)
	// One instance: edge-api inherits the deployment repo overlay environment.
	// The deployment repo itself stays provenance-only and must not materialize.
	if got := len(projection.InstanceRows); got != 1 {
		t.Fatalf("len(projection.InstanceRows) = %d, want 1", got)
	}
	if got := len(projection.DeploymentSourceRows); got != 1 {
		t.Fatalf("len(projection.DeploymentSourceRows) = %d, want 1", got)
	}

	intent := Intent{
		IntentID:        "intent-wm-runtime-argo",
		ScopeID:         "scope-edge-api",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-edge-api"},
		RelatedScopeIDs: []string{"scope-edge-api"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := result.CanonicalWrites; got == 0 {
		t.Fatal("CanonicalWrites = 0, want > 0")
	}
	if !recordedCallContainsParam(executor.calls, "deployment_repo_id", "repo-platform-deploy") {
		t.Fatalf("missing deployment_repo_id row for repo-platform-deploy; calls=%#v", executor.calls)
	}
	if !recordedCallContainsParam(executor.calls, "materialization_confidence", "0.96") {
		t.Fatal("missing materialization_confidence row for seeded runtime candidate")
	}
}

func TestWorkloadMaterializationHandlerUsesKustomizeDeploymentSourceReversedDirection(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo-app",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-my-api",
					"name":     "my-api",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-repo-deploy",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-helm-charts",
					"name":     "helm-charts",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file-app",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-my-api",
					"language":      "dockerfile",
					"relative_path": "Dockerfile",
					"parsed_file_data": map[string]any{
						"dockerfile_stages": []any{
							map[string]any{"name": "runtime"},
						},
					},
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file-deploy",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-helm-charts",
					"language":      "yaml",
					"relative_path": "overlays/production/my-api.yaml",
					"parsed_file_data": map[string]any{
						"k8s_resources": []any{
							map[string]any{"name": "my-api", "kind": "Deployment", "namespace": "apps"},
						},
					},
				},
				ObservedAt: now,
			},
		},
	}
	// Kustomize evidence: source=deploy_repo, target=app_repo (reversed from ArgoCD).
	relationshipLoader := &stubResolvedRelationshipLoader{
		resolved: []relationships.ResolvedRelationship{
			{
				SourceRepoID:     "repo-helm-charts",
				TargetRepoID:     "repo-my-api",
				RelationshipType: relationships.RelDeploysFrom,
				Confidence:       0.90,
				Details: map[string]any{
					"evidence_kinds": []any{
						string(relationships.EvidenceKindKustomizeResource),
					},
				},
			},
		},
	}

	executor := &recordingCypherExecutor{}
	handler := WorkloadMaterializationHandler{
		FactLoader:     loader,
		ResolvedLoader: relationshipLoader,
		Materializer:   NewWorkloadMaterializer(executor),
	}

	intent := Intent{
		IntentID:     "intent-kustomize-deploy",
		ScopeID:      "scope-my-api",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainWorkloadMaterialization,
		Cause:        "facts projected",
		EntityKeys:   []string{"repo-my-api"},
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0, want > 0")
	}
	// Verify my-api candidate got DeploymentRepoID = helm-charts (reversed direction).
	if !recordedCallContainsParam(executor.calls, "deployment_repo_id", "repo-helm-charts") {
		t.Fatal("missing deployment_repo_id=repo-helm-charts; Kustomize direction reversal failed")
	}
}

func TestWorkloadMaterializationHandlerSkipsUtilityOnlyCandidate(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-automation",
					"name":     "automation-shared",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-automation",
					"language":      "groovy",
					"relative_path": "Jenkinsfile",
					"parsed_file_data": map[string]any{
						"jenkins_pipeline_calls": []any{"deployShared"},
					},
				},
				ObservedAt: now,
			},
		},
	}
	executor := &recordingCypherExecutor{}
	handler := WorkloadMaterializationHandler{
		FactLoader:   loader,
		Materializer: NewWorkloadMaterializer(executor),
	}

	intent := Intent{
		IntentID:        "intent-wm-utility",
		ScopeID:         "scope-automation",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-automation"},
		RelatedScopeIDs: []string{"scope-automation"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := result.CanonicalWrites; got != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for utility-only candidate", got)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("len(executor.calls) = %d, want 0", got)
	}
}

func TestWorkloadMaterializationHandlerUsesPreCorrelatedInputLoader(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	inputLoader := &stubWorkloadProjectionInputLoader{
		candidates: []WorkloadCandidate{
			{
				RepoID:           "repo-precorrelated",
				RepoName:         "precorrelated-service",
				ResourceKinds:    []string{"deployment"},
				Namespaces:       []string{"production"},
				DeploymentRepoID: "repo-delivery",
				Classification:   "service",
				Confidence:       0.97,
				Provenance:       []string{"argocd_application_source"},
			},
		},
		deploymentEnvironments: map[string][]string{
			"repo-delivery": {"production"},
		},
	}
	factLoader := &stubFactLoader{}
	executor := &recordingCypherExecutor{}

	handler := WorkloadMaterializationHandler{
		FactLoader:   factLoader,
		InputLoader:  inputLoader,
		Materializer: NewWorkloadMaterializer(executor),
	}

	intent := Intent{
		IntentID:        "intent-wm-precorrelated",
		ScopeID:         "scope-precorrelated",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "precorrelated inputs ready",
		EntityKeys:      []string{"repo-precorrelated"},
		RelatedScopeIDs: []string{"scope-precorrelated"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0, want > 0")
	}
	if inputLoader.calls != 1 {
		t.Fatalf("InputLoader calls = %d, want 1", inputLoader.calls)
	}
	if factLoader.calls != 0 {
		t.Fatalf("FactLoader calls = %d, want 0 when pre-correlated inputs are provided", factLoader.calls)
	}
	if !recordedCallContainsParam(executor.calls, "deployment_repo_id", "repo-delivery") {
		t.Fatal("missing deployment_repo_id row for repo-delivery")
	}
}

func TestWorkloadMaterializationHandlerWritesProvisionedInfrastructureRuntimePlatforms(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	inputLoader := &stubWorkloadProjectionInputLoader{
		candidates: []WorkloadCandidate{
			{
				RepoID:              "repo-service",
				RepoName:            "service-api",
				ProvisioningRepoIDs: []string{"repo-infra"},
				ProvisioningEvidenceKinds: map[string][]string{
					"repo-infra": {TerraformPlatformEvidenceKind("ecs", "cluster")},
				},
				Classification: "service",
				Confidence:     0.96,
				Provenance:     []string{"dockerfile_runtime"},
			},
		},
		deploymentEnvironments: map[string][]string{
			"repo-infra": {"prod"},
		},
	}
	platformLookup := &stubInfrastructurePlatformLookup{
		platforms: map[string][]InfrastructurePlatformRow{
			"repo-infra": {
				{
					RepoID:           "repo-infra",
					PlatformID:       "platform:ecs:aws:cluster/runtime-main:none:none",
					PlatformName:     "runtime-main",
					PlatformKind:     "ecs",
					PlatformProvider: "aws",
					PlatformLocator:  "cluster/runtime-main",
				},
			},
		},
	}
	executor := &recordingCypherExecutor{}
	handler := WorkloadMaterializationHandler{
		FactLoader:                   &stubFactLoader{},
		InputLoader:                  inputLoader,
		InfrastructurePlatformLookup: platformLookup,
		Materializer:                 NewWorkloadMaterializer(executor),
	}

	intent := Intent{
		IntentID:        "intent-wm-infra-platform",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "replay after infrastructure platform",
		EntityKeys:      []string{"repo-service"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0, want > 0")
	}
	if got, want := platformLookup.repoIDs, []string{"repo-infra"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("platform lookup repoIDs = %v, want %v", got, want)
	}
	if !recordedCallContainsParam(executor.calls, "platform_kind", "ecs") {
		t.Fatal("missing ecs runtime platform row")
	}
	if !recordedCallContainsParam(executor.calls, "platform_id", "platform:ecs:aws:cluster/runtime-main:none:none") {
		t.Fatal("missing provisioned infrastructure platform id")
	}
}

func TestWorkloadMaterializationHandlerRejectsMissingDomain(t *testing.T) {
	t.Parallel()

	handler := WorkloadMaterializationHandler{
		FactLoader:   &stubFactLoader{},
		Materializer: NewWorkloadMaterializer(&recordingCypherExecutor{}),
	}

	intent := Intent{
		IntentID:        "intent-wm-3",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadIdentity,
		Cause:           "wrong domain",
		EntityKeys:      []string{"repo-a"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Now().UTC(),
		AvailableAt:     time.Now().UTC(),
		Status:          IntentStatusPending,
	}

	_, err := handler.Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle() expected error for wrong domain")
	}
}

func TestWorkloadMaterializationHandlerRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := WorkloadMaterializationHandler{
		Materializer: NewWorkloadMaterializer(&recordingCypherExecutor{}),
	}

	intent := Intent{
		IntentID:        "intent-wm-4",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-a"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Now().UTC(),
		AvailableAt:     time.Now().UTC(),
		Status:          IntentStatusPending,
	}

	_, err := handler.Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle() expected error for missing FactLoader")
	}
}

// -- test fakes --

type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}

type stubResolvedRelationshipLoader struct {
	resolved     []relationships.ResolvedRelationship
	repoResolved []relationships.ResolvedRelationship
	calls        int
	repoCalls    int
	repoIDs      []string
}

func (f *stubResolvedRelationshipLoader) GetResolvedRelationships(
	_ context.Context,
	_ string,
) ([]relationships.ResolvedRelationship, error) {
	f.calls++
	return f.resolved, nil
}

func (f *stubResolvedRelationshipLoader) GetResolvedRelationshipsForRepos(
	_ context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, error) {
	f.repoCalls++
	f.repoIDs = append([]string(nil), repoIDs...)
	return f.repoResolved, nil
}

type stubWorkloadProjectionInputLoader struct {
	candidates             []WorkloadCandidate
	deploymentEnvironments map[string][]string
	err                    error
	calls                  int
}

func (f *stubWorkloadProjectionInputLoader) LoadWorkloadProjectionInputs(
	_ context.Context,
	_ Intent,
) ([]WorkloadCandidate, map[string][]string, error) {
	f.calls++
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.candidates, f.deploymentEnvironments, nil
}

type stubInfrastructurePlatformLookup struct {
	platforms map[string][]InfrastructurePlatformRow
	repoIDs   []string
	err       error
}

func (f *stubInfrastructurePlatformLookup) ListProvisionedPlatforms(
	_ context.Context,
	repoIDs []string,
) (map[string][]InfrastructurePlatformRow, error) {
	f.repoIDs = append([]string(nil), repoIDs...)
	if f.err != nil {
		return nil, f.err
	}
	result := make(map[string][]InfrastructurePlatformRow)
	for _, repoID := range repoIDs {
		result[repoID] = append([]InfrastructurePlatformRow(nil), f.platforms[repoID]...)
	}
	return result, nil
}

type recordingCypherExecutor struct {
	calls []recordedCypherCall
}

type recordedCypherCall struct {
	cypher string
	params map[string]any
}

func (r *recordingCypherExecutor) ExecuteCypher(_ context.Context, cypher string, params map[string]any) error {
	r.calls = append(r.calls, recordedCypherCall{cypher: cypher, params: params})
	return nil
}

func TestWorkloadMaterializationHandlerFactLoaderError(t *testing.T) {
	t.Parallel()

	loader := &errorFactLoader{err: fmt.Errorf("db connection failed")}
	handler := WorkloadMaterializationHandler{
		FactLoader:   loader,
		Materializer: NewWorkloadMaterializer(&recordingCypherExecutor{}),
	}

	intent := Intent{
		IntentID:        "intent-wm-5",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-a"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Now().UTC(),
		AvailableAt:     time.Now().UTC(),
		Status:          IntentStatusPending,
	}

	_, err := handler.Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle() expected error from FactLoader")
	}
}

type errorFactLoader struct {
	err error
}

func (f *errorFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return nil, f.err
}

func containsRecordedCypher(calls []recordedCypherCall, fragment string) bool {
	for _, call := range calls {
		if strings.Contains(call.cypher, fragment) {
			return true
		}
	}
	return false
}

func recordedCallContainsParam(calls []recordedCypherCall, key, want string) bool {
	for _, call := range calls {
		rows, ok := call.params["rows"].([]map[string]any)
		if !ok {
			continue
		}
		for _, row := range rows {
			if fmt.Sprint(row[key]) == want {
				return true
			}
		}
	}
	return false
}
