package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCorrelatedWorkloadProjectionInputLoaderAdmitsServiceEdgeAPIStyleCandidate(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader: &stubFactLoader{
			envelopes: relationshipPlatformServiceEdgeAPIEnvelopes(now),
		},
	}

	intent := Intent{
		IntentID:        "intent-edge-loader",
		ScopeID:         "scope-edge",
		GenerationID:    "gen-edge",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo:service-edge-api"},
		RelatedScopeIDs: []string{"scope-edge"},
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
	if got, want := candidates[0].WorkloadName, "service-edge-api"; got != want {
		t.Fatalf("WorkloadName = %q, want %q", got, want)
	}
	if got, want := candidates[0].Classification, "service"; got != want {
		t.Fatalf("Classification = %q, want %q", got, want)
	}
	if got, want := candidates[0].Confidence, 0.98; got != want {
		t.Fatalf("Confidence = %v, want %v", got, want)
	}
	if got := deploymentEnvs["repository:r_service_edge_api"]; len(got) != 0 {
		t.Fatalf("deploymentEnvs[source-repo] = %v, want empty without overlay evidence", got)
	}
}

func TestWorkloadMaterializationHandlerWritesServiceEdgeAPIWorkloadWithoutEnvironmentInstance(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	handler := WorkloadMaterializationHandler{
		FactLoader: &stubFactLoader{
			envelopes: relationshipPlatformServiceEdgeAPIEnvelopes(now),
		},
		Materializer: NewWorkloadMaterializer(&recordingCypherExecutor{}),
	}

	recordingExecutor, ok := handler.Materializer.CypherExecutor().(*recordingCypherExecutor)
	if !ok {
		t.Fatal("expected recordingCypherExecutor")
	}

	intent := Intent{
		IntentID:        "intent-edge-materialize",
		ScopeID:         "scope-edge",
		GenerationID:    "gen-edge",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo:service-edge-api"},
		RelatedScopeIDs: []string{"scope-edge"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got, want := result.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if !containsRecordedCypher(recordingExecutor.calls, "MERGE (w:Workload {id: row.workload_id})") {
		t.Fatal("missing workload upsert cypher")
	}
	if recordedCallContainsParam(recordingExecutor.calls, "instance_id", "workload-instance:service-edge-api:modern") {
		t.Fatal("unexpected instance upsert for unrecognized environment namespace")
	}
	if !recordedCallContainsParam(recordingExecutor.calls, "workload_name", "service-edge-api") {
		t.Fatal("missing workload_name row for service-edge-api")
	}
}

func relationshipPlatformServiceEdgeAPIEnvelopes(now time.Time) []facts.Envelope {
	return []facts.Envelope{
		{
			FactID:   "fact-repo-edge",
			ScopeID:  "scope-edge",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repository:r_service_edge_api",
				"name":     "service-edge-api",
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-dockerfile-edge",
			ScopeID:  "scope-edge",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repository:r_service_edge_api",
				"language":      "dockerfile",
				"relative_path": "Dockerfile",
				"parsed_file_data": map[string]any{
					"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
				},
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-jenkins-edge",
			ScopeID:  "scope-edge",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repository:r_service_edge_api",
				"language":         "groovy",
				"relative_path":    "Jenkinsfile",
				"parsed_file_data": map[string]any{},
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-actions-edge",
			ScopeID:  "scope-edge",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repository:r_service_edge_api",
				"language":      "yaml",
				"relative_path": ".github/workflows/deploy-legacy.yml",
				"parsed_file_data": map[string]any{
					"github_actions_workflow_triggers": []any{"push"},
					"github_actions_reusable_workflow_refs": []any{
						map[string]any{"uses": "eshu-hq/delivery-legacy-automation/.github/workflows/promote-edge-api.yml@main"},
					},
				},
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-compose-edge",
			ScopeID:  "scope-edge",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repository:r_service_edge_api",
				"language":      "yaml",
				"relative_path": "docker-compose.yaml",
				"parsed_file_data": map[string]any{
					"docker_compose_services": []any{
						map[string]any{"service_name": "edge-api"},
						map[string]any{"service_name": "service-worker-jobs"},
					},
				},
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-k8s-edge",
			ScopeID:  "scope-edge",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repository:r_service_edge_api",
				"language":      "yaml",
				"relative_path": "k8s/deployment.yaml",
				"parsed_file_data": map[string]any{
					"k8s_resources": []any{
						map[string]any{
							"kind":      "Deployment",
							"name":      "service-edge-api",
							"namespace": "modern",
						},
					},
				},
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-cloudformation-edge",
			ScopeID:  "scope-edge",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repository:r_service_edge_api",
				"language":      "yaml",
				"relative_path": "infra/api-service.yaml",
				"parsed_file_data": map[string]any{
					"cloudformation_resources": []any{
						map[string]any{"type": "AWS::ECS::Service"},
					},
				},
			},
			ObservedAt: now,
		},
	}
}
