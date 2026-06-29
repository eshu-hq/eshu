// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCodeFlowCFGSummarySurfacesExactParserFactsAndBounds(t *testing.T) {
	t.Parallel()

	store := &fakeCodeFlowStore{model: CodeFlowReadModel{
		Freshness: FreshnessFresh,
		Functions: []CodeFlowFunction{
			{
				RepoID: "repo-1", RelativePath: "src/handler.go", FunctionName: "handle",
				FunctionUID: "func-handle", Language: "go", LineNumber: 3,
				CFGBlocks: []any{
					map[string]any{"id": 0, "kind": "entry"},
					map[string]any{"id": 1, "kind": "exit"},
				},
				CFGEdges:            []any{map[string]any{"from": 0, "to": 1}},
				DefUse:              []map[string]any{{"binding": "q", "def_line": 4, "use_line": 5}},
				ControlDependencies: []map[string]any{{"controller": 4, "dependent": 5}},
				EvidenceHandle:      "fact://code_dataflow_function/func-handle",
				SourceGenerationID:  "gen-1",
				SourceObservedAt:    time.Date(2026, time.June, 28, 1, 0, 0, 0, time.UTC),
			},
			{RepoID: "repo-1", RelativePath: "src/other.go", FunctionName: "other", Language: "go"},
		},
	}}
	rec := callCodeFlowEndpoint(t, &CodeHandler{
		CodeFlow: store,
		Profile:  ProfileLocalAuthoritative,
	}, "/api/v0/code/flow/cfg-summary", `{"repo_id":"repo-1","language":"go","symbol":"handle","limit":1}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	env := decodeCodeFlowEnvelope(t, rec)
	if env.Truth == nil || env.Truth.Capability != codeFlowCFGSummaryCapability {
		t.Fatalf("truth = %#v, want cfg-summary capability", env.Truth)
	}
	data := env.Data.(map[string]any)
	functions := data["functions"].([]any)
	if got, want := len(functions), 1; got != want {
		t.Fatalf("len(functions) = %d, want %d", got, want)
	}
	first := functions[0].(map[string]any)
	if got, want := first["fact_label"], "exact_parser_fact"; got != want {
		t.Fatalf("fact_label = %#v, want %#v", got, want)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["state"], "exact"; got != want {
		t.Fatalf("coverage.state = %#v, want %#v", got, want)
	}
	bounds := data["bounds"].(map[string]any)
	if got, want := bounds["truncated"], true; got != want {
		t.Fatalf("bounds.truncated = %#v, want %#v", got, want)
	}
	if store.last.Kind != CodeFlowKindCFGSummary || store.last.RepoID != "repo-1" || store.last.Limit != 2 {
		t.Fatalf("filter = %+v, want cfg repo-1 limit+1 probe", store.last)
	}
}

func TestCodeFlowTaintPathSurfacesDerivedEvidence(t *testing.T) {
	t.Parallel()

	store := &fakeCodeFlowStore{model: CodeFlowReadModel{
		Freshness: FreshnessFresh,
		TaintPaths: []CodeFlowTaintPath{{
			RepoID: "repo-1", RelativePath: "src/handler.go", FunctionName: "handle",
			Language: "go", SourceKind: "http_request", SinkKind: "sql",
			SourceLine: 4, SinkLine: 5, Confidence: 0.8,
			EvidenceHandle: "fact://code_taint_evidence/fact-1",
		}},
	}}
	rec := callCodeFlowEndpoint(t, &CodeHandler{
		CodeFlow: store,
		Profile:  ProfileLocalAuthoritative,
	}, "/api/v0/code/flow/taint-path", `{"repo_id":"repo-1","language":"go","symbol":"handle","limit":5}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	env := decodeCodeFlowEnvelope(t, rec)
	if env.Truth == nil || env.Truth.Level != TruthLevelDerived {
		t.Fatalf("truth level = %#v, want derived", env.Truth)
	}
	data := env.Data.(map[string]any)
	paths := data["paths"].([]any)
	if got, want := len(paths), 1; got != want {
		t.Fatalf("len(paths) = %d, want %d", got, want)
	}
	path := paths[0].(map[string]any)
	if got, want := path["fact_label"], "derived_reducer_evidence"; got != want {
		t.Fatalf("fact_label = %#v, want %#v", got, want)
	}
	if got, want := path["evidence_handle"], "fact://code_taint_evidence/fact-1"; got != want {
		t.Fatalf("evidence_handle = %#v, want %#v", got, want)
	}
}

func TestCodeFlowReachingDefUnsupportedLanguageReturnsExplicitUnsupportedCoverage(t *testing.T) {
	t.Parallel()

	store := &fakeCodeFlowStore{}
	rec := callCodeFlowEndpoint(t, &CodeHandler{
		CodeFlow: store,
		Profile:  ProfileLocalAuthoritative,
	}, "/api/v0/code/flow/reaching-def", `{"repo_id":"repo-1","language":"ruby","symbol":"handle","limit":5}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if store.called {
		t.Fatal("store called for unsupported language, want short-circuit")
	}
	env := decodeCodeFlowEnvelope(t, rec)
	data := env.Data.(map[string]any)
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["state"], "unsupported"; got != want {
		t.Fatalf("coverage.state = %#v, want %#v", got, want)
	}
	if got, want := coverage["language"], "ruby"; got != want {
		t.Fatalf("coverage.language = %#v, want %#v", got, want)
	}
	bounds := data["bounds"].(map[string]any)
	if got, want := bounds["limit"], float64(5); got != want {
		t.Fatalf("bounds.limit = %#v, want %#v", got, want)
	}
}

func TestCodeFlowPDGSummaryAmbiguousSymbolAndStaleGenerationStayExplicit(t *testing.T) {
	t.Parallel()

	store := &fakeCodeFlowStore{model: CodeFlowReadModel{
		Freshness:       FreshnessStale,
		FreshnessDetail: "active generation is older than repository head",
		Functions: []CodeFlowFunction{
			{RepoID: "repo-1", RelativePath: "a.go", FunctionName: "handle", FunctionUID: "func-a", Language: "go", LineNumber: 3},
			{RepoID: "repo-1", RelativePath: "b.go", FunctionName: "handle", FunctionUID: "func-b", Language: "go", LineNumber: 9},
		},
	}}
	rec := callCodeFlowEndpoint(t, &CodeHandler{
		CodeFlow: store,
		Profile:  ProfileLocalAuthoritative,
	}, "/api/v0/code/flow/pdg-summary", `{"repo_id":"repo-1","language":"go","symbol":"handle","limit":10}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	env := decodeCodeFlowEnvelope(t, rec)
	if got, want := env.Truth.Freshness.State, FreshnessStale; got != want {
		t.Fatalf("truth freshness = %q, want %q", got, want)
	}
	data := env.Data.(map[string]any)
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["state"], "partial"; got != want {
		t.Fatalf("coverage.state = %#v, want %#v", got, want)
	}
	ambiguity := data["ambiguity"].(map[string]any)
	if got, want := ambiguity["ambiguous"], true; got != want {
		t.Fatalf("ambiguity.ambiguous = %#v, want %#v", got, want)
	}
	candidates := ambiguity["candidates"].([]any)
	if got, want := len(candidates), 2; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
}

func TestCodeFlowScopedRepositoryFilterIsAppliedBeforeStoreRead(t *testing.T) {
	t.Parallel()

	store := &fakeCodeFlowStore{}
	handler := &CodeHandler{CodeFlow: store, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/flow/cfg-summary",
		bytes.NewBufferString(`{"repo_id":"repo-team-a","language":"go","limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.handleCFGSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !store.called || store.last.RepoID != "repo-team-a" {
		t.Fatalf("store called=%v filter=%+v, want scoped repo-team-a read", store.called, store.last)
	}
}

type fakeCodeFlowStore struct {
	called bool
	last   CodeFlowFilter
	model  CodeFlowReadModel
	err    error
}

func (s *fakeCodeFlowStore) ListCodeFlow(ctx context.Context, filter CodeFlowFilter) (CodeFlowReadModel, error) {
	s.called = true
	s.last = filter
	return s.model, s.err
}

func callCodeFlowEndpoint(t *testing.T, handler *CodeHandler, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	switch path {
	case "/api/v0/code/flow/taint-path":
		handler.handleTaintPath(rec, req)
	case "/api/v0/code/flow/reaching-def":
		handler.handleReachingDef(rec, req)
	case "/api/v0/code/flow/cfg-summary":
		handler.handleCFGSummary(rec, req)
	case "/api/v0/code/flow/pdg-summary":
		handler.handlePDGSummary(rec, req)
	default:
		t.Fatalf("unknown code-flow test path %q", path)
	}
	return rec
}

func decodeCodeFlowEnvelope(t *testing.T, rec *httptest.ResponseRecorder) ResponseEnvelope {
	t.Helper()
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("env.Data type = %T, want map[string]any", env.Data)
	}
	env.Data = data
	return env
}
