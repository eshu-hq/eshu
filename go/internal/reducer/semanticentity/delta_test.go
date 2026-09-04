// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticentity

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func TestSemanticEntityMaterializationHandlerScopesDeltaRetractToFiles(t *testing.T) {
	t.Parallel()

	loader := &stubKindFactLoader{
		byKind: []facts.Envelope{
			{
				ScopeID:  "scope-1",
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":                      "repo-1",
					"path":                         "/repo",
					"source_run_id":                "source-run-1",
					"delta_generation":             true,
					"delta_relative_paths":         []string{"src/changed.go", "src/deleted .go"},
					"delta_deleted_relative_paths": []any{"src/deleted .go"},
				},
			},
			{
				ScopeID:  "scope-1",
				FactKind: "content_entity",
				SourceRef: facts.Ref{
					SourceURI: "/repo/src/changed.go",
				},
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"entity_id":     "module-1",
					"entity_type":   "Module",
					"entity_name":   "changed",
					"language":      "go",
					"relative_path": "src/changed.go",
				},
			},
		},
	}
	writer := &recordingSemanticEntityWriter{result: SemanticEntityWriteResult{CanonicalWrites: 1}}
	handler := SemanticEntityMaterializationHandler{
		FactLoader:           loader,
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-delta-semantic",
		ScopeID:      "scope-1",
		GenerationID: "generation-2",
		Domain:       reducercontract.DomainSemanticEntityMaterialization,
		EntityKeys:   []string{"repo:repo-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(writer.writes), 1; got != want {
		t.Fatalf("semantic writes = %d, want %d", got, want)
	}
	write := writer.writes[0]
	if !write.DeltaProjection {
		t.Fatal("DeltaProjection = false, want true")
	}
	if got, want := write.DeltaFilePaths, []string{"/repo/src/changed.go", "/repo/src/deleted .go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaFilePaths = %#v, want %#v", got, want)
	}
	if write.SkipRetract {
		t.Fatal("SkipRetract = true, want false for delta generation with prior state")
	}
}

func TestSemanticEntityMaterializationHandlerScopesDeletedOnlyDeltaRetract(t *testing.T) {
	t.Parallel()

	loader := &stubKindFactLoader{
		byKind: []facts.Envelope{
			{
				ScopeID:  "scope-1",
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":                      "repo-1",
					"path":                         "/repo",
					"source_run_id":                "source-run-1",
					"delta_generation":             true,
					"delta_relative_paths":         []string{"src/deleted.go"},
					"delta_deleted_relative_paths": []string{"src/deleted.go"},
				},
			},
		},
	}
	writer := &recordingSemanticEntityWriter{}
	handler := SemanticEntityMaterializationHandler{
		FactLoader:           loader,
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-delta-semantic-delete",
		ScopeID:      "scope-1",
		GenerationID: "generation-2",
		Domain:       reducercontract.DomainSemanticEntityMaterialization,
		EntityKeys:   []string{"repo:repo-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := result.EvidenceSummary, "retracted semantic entities across 1 repositories"; got != want {
		t.Fatalf("EvidenceSummary = %q, want %q", got, want)
	}
	if got, want := len(writer.writes), 1; got != want {
		t.Fatalf("semantic writes = %d, want %d", got, want)
	}
	write := writer.writes[0]
	if !write.DeltaProjection {
		t.Fatal("DeltaProjection = false, want true")
	}
	if got, want := write.DeltaFilePaths, []string{"/repo/src/deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaFilePaths = %#v, want %#v", got, want)
	}
	if got, want := len(write.Rows), 0; got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
}

// stubKindFactLoader is a minimal local copy of the reducer root's
// recordingKindFactLoader (fact_kind_loader_test.go), scoped to this
// package: it implements both factload.FactLoader and the optional
// factload.FactKindLoader extension so LoadFactsForKinds exercises the
// ListFactsByKind path the delta-scope tests above depend on.
type stubKindFactLoader struct {
	byKind []facts.Envelope
}

func (l *stubKindFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return nil, nil
}

func (l *stubKindFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	_ []string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.byKind...), nil
}
