package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type recordingWorkloadDependencyEdgeWriter struct {
	retractCalls []sharedEdgeWriteCall
	writeCalls   []sharedEdgeWriteCall
}

type sharedEdgeWriteCall struct {
	domain         string
	evidenceSource string
	rows           []SharedProjectionIntentRow
}

func (r *recordingWorkloadDependencyEdgeWriter) RetractEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) error {
	r.retractCalls = append(r.retractCalls, sharedEdgeWriteCall{
		domain:         domain,
		evidenceSource: evidenceSource,
		rows:           append([]SharedProjectionIntentRow(nil), rows...),
	})
	return nil
}

func (r *recordingWorkloadDependencyEdgeWriter) WriteEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) error {
	r.writeCalls = append(r.writeCalls, sharedEdgeWriteCall{
		domain:         domain,
		evidenceSource: evidenceSource,
		rows:           append([]SharedProjectionIntentRow(nil), rows...),
	})
	return nil
}

func TestWorkloadMaterializationHandlerReconcilesWorkloadDependencies(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactID:   "fact-repo",
				FactKind: "repository",
				Payload: map[string]any{
					"graph_id": "repo-service-a",
					"name":     "service-a",
				},
				ObservedAt: now,
			},
			{
				FactID:   "fact-file",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id": "repo-service-a",
					"parsed_file_data": map[string]any{
						"k8s_resources": []any{
							map[string]any{
								"name":      "service-a",
								"kind":      "Deployment",
								"namespace": "modern",
							},
						},
					},
				},
				ObservedAt: now,
			},
		},
	}
	dependencyLookup := &fakeWorkloadDependencyGraphLookup{
		repoEdges: []RepoDependencyEdge{
			{SourceRepoID: "repo-service-a", TargetRepoID: "repo-service-b"},
		},
		workloads: []RepoWorkload{
			{RepoID: "repo-service-b", WorkloadID: "workload:service-b"},
		},
		workloadDependencyRows: []ExistingWorkloadDependencyEdge{
			{
				RepoID:           "repo-service-a",
				WorkloadID:       "workload:service-a",
				TargetWorkloadID: "workload:old-service",
			},
		},
	}
	edgeWriter := &recordingWorkloadDependencyEdgeWriter{}

	handler := WorkloadMaterializationHandler{
		FactLoader:                   loader,
		Materializer:                 NewWorkloadMaterializer(&recordingCypherExecutor{}),
		DependencyLookup:             dependencyLookup,
		WorkloadDependencyEdgeWriter: edgeWriter,
	}

	intent := Intent{
		IntentID:     "intent-wm-deps",
		ScopeID:      "scope-service-a",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainWorkloadMaterialization,
		Cause:        "facts projected",
		EntityKeys:   []string{"repo-service-a"},
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got, want := len(edgeWriter.retractCalls), 1; got != want {
		t.Fatalf("len(retractCalls) = %d, want %d", got, want)
	}
	if got, want := len(edgeWriter.writeCalls), 1; got != want {
		t.Fatalf("len(writeCalls) = %d, want %d", got, want)
	}

	retractRow := edgeWriter.retractCalls[0].rows[0]
	if got, want := retractRow.RepositoryID, "repo-service-a"; got != want {
		t.Fatalf("retract RepositoryID = %q, want %q", got, want)
	}

	writeRow := edgeWriter.writeCalls[0].rows[0]
	if got, want := payloadStringAny(writeRow.Payload, "workload_id"), "workload:service-a"; got != want {
		t.Fatalf("payload.workload_id = %q, want %q", got, want)
	}
	if got, want := payloadStringAny(writeRow.Payload, "target_workload_id"), "workload:service-b"; got != want {
		t.Fatalf("payload.target_workload_id = %q, want %q", got, want)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0, want > 0")
	}
}
