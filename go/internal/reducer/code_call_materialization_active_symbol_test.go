// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCodeCallMaterializationHandlerLoadsActiveCrossRepoSymbolDefinitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 9, 0, 0, 0, time.UTC)
	scipSymbol := "scip-go gomod github.com/acme/lib Client#Request()."
	loader := &activeCodeCallSymbolFactLoader{
		primary: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":       "repo-app",
					"source_run_id": "run-app",
					"graph_id":      "repo-app",
					"graph_kind":    "repository",
				},
			},
			{
				FactKind: factKindFile,
				Payload: map[string]any{
					"repo_id":       "repo-app",
					"relative_path": "cmd/app/main.go",
					"parsed_file_data": map[string]any{
						"path": "/workspace/app/cmd/app/main.go",
						"functions": []any{
							map[string]any{
								"name":        "main",
								"line_number": 10,
								"end_line":    16,
								"uid":         "uid:app:main",
							},
						},
						"function_calls_scip": []any{
							map[string]any{
								"caller_file":   "/workspace/app/cmd/app/main.go",
								"caller_line":   10,
								"caller_symbol": "scip-go gomod github.com/acme/app main().",
								"callee_symbol": scipSymbol,
								"ref_line":      13,
							},
						},
					},
				},
			},
		},
		activeDefinitions: []facts.Envelope{
			{
				FactKind: factKindFile,
				Payload: map[string]any{
					"repo_id":       "repo-lib",
					"relative_path": "client.go",
					"parsed_file_data": map[string]any{
						"path": "/workspace/lib/client.go",
						"functions": []any{
							map[string]any{
								"name":        "Request",
								"line_number": 4,
								"end_line":    8,
								"uid":         "uid:lib:request",
								"scip_symbol": scipSymbol,
							},
						},
					},
				},
			},
		},
	}
	writer := &recordingCodeCallIntentWriter{}
	handler := CodeCallMaterializationHandler{
		FactLoader:   loader,
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-cross-repo-symbol",
		ScopeID:      "scope-app",
		GenerationID: "gen-app",
		SourceSystem: "git",
		Domain:       DomainCodeCallMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got, want := loader.activeSymbolCalls, 1; got != want {
		t.Fatalf("active symbol loader calls = %d, want %d", got, want)
	}
	if want := []string{scipSymbol}; !reflect.DeepEqual(loader.requestedSymbols, want) {
		t.Fatalf("requested symbols = %#v, want %#v", loader.requestedSymbols, want)
	}

	var codeCallRows []SharedProjectionIntentRow
	for _, row := range writer.rows {
		if row.RepositoryID == "repo-lib" {
			t.Fatalf("external definition repo emitted an intent: %#v", row)
		}
		if row.Payload["evidence_source"] == codeCallEvidenceSource {
			codeCallRows = append(codeCallRows, row)
		}
	}
	if got, want := len(codeCallRows), 1; got != want {
		t.Fatalf("code-call intent rows = %d, want %d; rows=%#v", got, want, writer.rows)
	}
	if got, want := codeCallRows[0].RepositoryID, "repo-app"; got != want {
		t.Fatalf("code-call RepositoryID = %q, want %q", got, want)
	}
	if got, want := codeCallRows[0].Payload["caller_entity_id"], "uid:app:main"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := codeCallRows[0].Payload["callee_entity_id"], "uid:lib:request"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

type activeCodeCallSymbolFactLoader struct {
	primary           []facts.Envelope
	activeDefinitions []facts.Envelope
	requestedSymbols  []string
	activeSymbolCalls int
}

func (l *activeCodeCallSymbolFactLoader) ListFacts(
	_ context.Context,
	_ string,
	_ string,
) ([]facts.Envelope, error) {
	return l.primary, nil
}

func (l *activeCodeCallSymbolFactLoader) LoadActiveCodeCallSymbolDefinitionFacts(
	_ context.Context,
	symbolKeys []string,
) ([]facts.Envelope, error) {
	l.activeSymbolCalls++
	l.requestedSymbols = append(l.requestedSymbols, symbolKeys...)
	return l.activeDefinitions, nil
}
