package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSQLRelationshipHandlerUsesKindFilteredFactLoader(t *testing.T) {
	t.Parallel()

	loader := &recordingKindFactLoader{
		byKind: []facts.Envelope{
			{
				FactKind: "content_entity",
				Payload: map[string]any{
					"repo_id":     "repo-1",
					"entity_id":   "content-entity:table-1",
					"entity_type": "SqlTable",
					"entity_name": "public.users",
				},
			},
		},
		all: []facts.Envelope{
			{FactKind: "file"},
		},
	}
	writer := &recordingSQLRelEdgeWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader:           loader,
		EdgeWriter:           writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-kind-filter",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.listFactsCalls != 0 {
		t.Fatalf("ListFacts() calls = %d, want 0", loader.listFactsCalls)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "content_entity"; got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := len(writer.retractRows), 0; got != want {
		t.Fatalf("retract count = %d, want %d", got, want)
	}
}

func TestSemanticEntityMaterializationHandlerUsesKindFilteredFactLoader(t *testing.T) {
	t.Parallel()

	loader := &recordingKindFactLoader{
		byKind: []facts.Envelope{
			{
				ScopeID:  "scope-1",
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"source_run_id": "source-run-1",
				},
			},
			{
				FactKind: "content_entity",
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"entity_id":     "entity-1",
					"entity_type":   "Module",
					"entity_name":   "main",
					"language":      "go",
					"relative_path": "main.go",
				},
				SourceRef: facts.Ref{SourceURI: "file:///repo/main.go"},
			},
		},
		all: []facts.Envelope{
			{FactKind: "file"},
		},
	}
	writer := &recordingSemanticEntityWriter{result: SemanticEntityWriteResult{CanonicalWrites: 1}}
	publisher := &recordingSemanticEntityPhasePublisher{}
	handler := SemanticEntityMaterializationHandler{
		FactLoader:           loader,
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
		PhasePublisher:       publisher,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-semantic-kind-filter",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Domain:       DomainSemanticEntityMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.listFactsCalls != 0 {
		t.Fatalf("ListFacts() calls = %d, want 0", loader.listFactsCalls)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "repository,content_entity"; got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := len(writer.writes), 1; got != want {
		t.Fatalf("semantic writes = %d, want %d", got, want)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("phase publish calls = %d, want %d", got, want)
	}
}

func TestCorrelatedWorkloadProjectionInputLoaderUsesKindFilteredFactLoader(t *testing.T) {
	t.Parallel()

	loader := &recordingKindFactLoader{
		byKind: []facts.Envelope{
			{
				FactKind:  "repository",
				SourceRef: facts.Ref{SourceSystem: "git"},
				Payload: map[string]any{
					"graph_id": "repo-1",
					"name":     "payments",
				},
			},
			{
				FactKind:  "file",
				SourceRef: facts.Ref{SourceSystem: "git"},
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"relative_path": "deploy/payment.yaml",
					"parsed_file_data": map[string]any{
						"k8s_resources": []any{
							map[string]any{"kind": "Deployment", "namespace": "prod"},
						},
					},
				},
			},
		},
		all: []facts.Envelope{
			{FactKind: "content_entity"},
		},
	}
	inputLoader := CorrelatedWorkloadProjectionInputLoader{FactLoader: loader}

	candidates, _, err := inputLoader.LoadWorkloadProjectionInputs(context.Background(), Intent{
		IntentID:     "intent-workload-kind-filter",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		SourceSystem: "git",
		Domain:       DomainWorkloadMaterialization,
		EntityKeys:   []string{"repo-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v, want nil", err)
	}
	if loader.listFactsCalls != 0 {
		t.Fatalf("ListFacts() calls = %d, want 0", loader.listFactsCalls)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "repository,file"; got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("candidates = %d, want %d", got, want)
	}
}

func TestPlatformMaterializationHandlerUsesKindFilteredFactLoaderForInfrastructure(t *testing.T) {
	t.Parallel()

	loader := &recordingKindFactLoader{
		byKind: []facts.Envelope{
			{
				ScopeID:  "scope-1",
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id": "repo-1",
					"name":    "infra",
				},
			},
			{
				ScopeID:  "scope-1",
				FactKind: "file",
				Payload: map[string]any{
					"repo_id": "repo-1",
					"parsed_file_data": map[string]any{
						"terraform_resources": []any{
							map[string]any{
								"resource_type": "aws_eks_cluster",
								"resource_name": "main",
							},
						},
					},
				},
			},
		},
		all: []facts.Envelope{
			{FactKind: "content_entity"},
		},
	}
	handler := PlatformMaterializationHandler{
		Writer: &recordingPlatformMaterializationWriter{
			result: PlatformMaterializationWriteResult{CanonicalWrites: 1},
		},
		FactLoader:                 loader,
		InfrastructureMaterializer: NewInfrastructurePlatformMaterializer(&recordingCypherExecutor{}),
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-platform-kind-filter",
		ScopeID:         "scope-1",
		GenerationID:    "generation-1",
		Domain:          DomainDeploymentMapping,
		EntityKeys:      []string{"repo:repo-1"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Now(),
		AvailableAt:     time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.listFactsCalls != 0 {
		t.Fatalf("ListFacts() calls = %d, want 0", loader.listFactsCalls)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "repository,file,parsed_file_data"; got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
}

func TestCodeCallMaterializationHandlerUsesKindFilteredFactLoader(t *testing.T) {
	t.Parallel()

	loader := &recordingKindFactLoader{
		byKind: []facts.Envelope{
			{
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"source_run_id": "source-run-1",
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"relative_path": "caller.go",
					"parsed_file_data": map[string]any{
						"path": "caller.go",
						"functions": []any{
							map[string]any{
								"name":        "caller",
								"line_number": 10,
								"uid":         "entity:caller",
							},
						},
					},
				},
			},
		},
		all: []facts.Envelope{
			{FactKind: "content_entity"},
		},
	}
	writer := &recordingCodeCallIntentWriter{}
	handler := CodeCallMaterializationHandler{
		FactLoader:   loader,
		IntentWriter: writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-code-call-kind-filter",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Domain:       DomainCodeCallMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.listFactsCalls != 0 {
		t.Fatalf("ListFacts() calls = %d, want 0", loader.listFactsCalls)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "repository,file"; got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := len(writer.rows), 1; got != want {
		t.Fatalf("intent rows = %d, want %d", got, want)
	}
}

func TestDeployableUnitCorrelationHandlerUsesKindFilteredFactLoader(t *testing.T) {
	t.Parallel()

	loader := &recordingKindFactLoader{
		byKind: deployableUnitCorrelationEnvelopes(
			"repo-1",
			"payments",
			[]map[string]any{
				{
					"repo_id":       "repo-1",
					"relative_path": "deploy/payment.yaml",
					"parsed_file_data": map[string]any{
						"k8s_resources": []any{
							map[string]any{"kind": "Deployment", "namespace": "prod"},
						},
					},
				},
			},
		),
		all: []facts.Envelope{
			{FactKind: "content_entity"},
		},
	}
	handler := DeployableUnitCorrelationHandler{FactLoader: loader}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-deployable-kind-filter",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		SourceSystem: "git",
		Domain:       DomainDeployableUnitCorrelation,
		EntityKeys:   []string{"repo-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.listFactsCalls != 0 {
		t.Fatalf("ListFacts() calls = %d, want 0", loader.listFactsCalls)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "repository,file"; got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
}

func TestInheritanceMaterializationHandlerUsesKindFilteredFactLoader(t *testing.T) {
	t.Parallel()

	loader := &recordingKindFactLoader{
		byKind: inheritanceEntityFacts(),
		all: []facts.Envelope{
			{FactKind: "file"},
		},
	}
	writer := &recordingInheritanceEdgeWriter{}
	handler := InheritanceMaterializationHandler{
		FactLoader:           loader,
		EdgeWriter:           writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-inheritance-kind-filter",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Domain:       DomainInheritanceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.listFactsCalls != 0 {
		t.Fatalf("ListFacts() calls = %d, want 0", loader.listFactsCalls)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "content_entity"; got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := len(writer.writeRows), 1; got != want {
		t.Fatalf("inheritance write rows = %d, want %d", got, want)
	}
}

type recordingKindFactLoader struct {
	all            []facts.Envelope
	byKind         []facts.Envelope
	listFactsCalls int
	kindCalls      [][]string
}

func (l *recordingKindFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	l.listFactsCalls++
	return append([]facts.Envelope(nil), l.all...), nil
}

func (l *recordingKindFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	factKinds []string,
) ([]facts.Envelope, error) {
	l.kindCalls = append(l.kindCalls, append([]string(nil), factKinds...))
	return append([]facts.Envelope(nil), l.byKind...), nil
}
