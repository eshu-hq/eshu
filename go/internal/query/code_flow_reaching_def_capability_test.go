// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"testing"
	"time"
)

// TestCodeFlowReachingDefSurfacesExactDefUseFactsForSupportedLanguage proves
// the code_flow.reaching_def capability's positive path end to end through
// the real POST /api/v0/code/flow/reaching-def handler: for a supported
// language (go) with active parser dataflow facts, the handler must return
// the def-use rows labeled exact_parser_fact with coverage state "exact".
// The only existing reaching-def coverage
// (TestCodeFlowReachingDefUnsupportedLanguageReturnsExplicitUnsupportedCoverage)
// exercises the unsupported-language short-circuit and never reaches
// buildCodeFlowPayload's CodeFlowKindReachingDef branch (issue #5681
// cluster A).
func TestCodeFlowReachingDefSurfacesExactDefUseFactsForSupportedLanguage(t *testing.T) {
	t.Parallel()

	store := &fakeCodeFlowStore{model: CodeFlowReadModel{
		Freshness: FreshnessFresh,
		Functions: []CodeFlowFunction{
			{
				RepoID: "repo-1", RelativePath: "src/handler.go", FunctionName: "handle",
				FunctionUID: "func-handle", Language: "go", LineNumber: 3,
				DefUse: []map[string]any{
					{"binding": "req", "def_line": 4, "use_line": 6},
					{"binding": "resp", "def_line": 5, "use_line": 7},
				},
				EvidenceHandle:     "fact://code_dataflow_function/func-handle",
				SourceGenerationID: "gen-1",
				SourceObservedAt:   time.Date(2026, time.June, 28, 1, 0, 0, 0, time.UTC),
			},
		},
	}}
	rec := callCodeFlowEndpoint(t, &CodeHandler{
		CodeFlow: store,
		Profile:  ProfileLocalAuthoritative,
	}, "/api/v0/code/flow/reaching-def", `{"repo_id":"repo-1","language":"go","symbol":"handle","limit":5}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !store.called {
		t.Fatal("store not called, want reaching-def read for supported language")
	}
	if got, want := store.last.Kind, CodeFlowKindReachingDef; got != want {
		t.Fatalf("filter.Kind = %q, want %q", got, want)
	}

	env := decodeCodeFlowEnvelope(t, rec)
	if env.Truth == nil || env.Truth.Capability != codeFlowReachingDefCapability {
		t.Fatalf("truth = %#v, want reaching-def capability", env.Truth)
	}

	data := env.Data.(map[string]any)
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["state"], "exact"; got != want {
		t.Fatalf("coverage.state = %#v, want %#v", got, want)
	}

	definitions, ok := data["definitions"].([]any)
	if !ok || len(definitions) != 1 {
		t.Fatalf("data[definitions] = %#v, want one function's def-use rows", data["definitions"])
	}
	first, ok := definitions[0].(map[string]any)
	if !ok {
		t.Fatalf("definitions[0] type = %T, want map[string]any", definitions[0])
	}
	if got, want := first["fact_label"], "exact_parser_fact"; got != want {
		t.Fatalf("definitions[0][fact_label] = %#v, want %#v", got, want)
	}
	defUse, ok := first["def_use"].([]any)
	if !ok {
		t.Fatalf("definitions[0][def_use] type = %T, want []any", first["def_use"])
	}
	if got, want := len(defUse), 2; got != want {
		t.Fatalf("len(definitions[0][def_use]) = %d, want %d: %#v", got, want, defUse)
	}
	firstBinding, ok := defUse[0].(map[string]any)
	if !ok {
		t.Fatalf("def_use[0] type = %T, want map[string]any", defUse[0])
	}
	if got, want := firstBinding["binding"], "req"; got != want {
		t.Fatalf("def_use[0][binding] = %#v, want %#v", got, want)
	}
}
