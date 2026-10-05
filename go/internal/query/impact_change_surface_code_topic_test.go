// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// decodeChangeSurfaceCodeTopicData mirrors impact's decodeChangeSurfaceData
// for this root-staying test: the shared helper moved with the family to
// impact/, and this is its only root caller. See #6060.
func decodeChangeSurfaceCodeTopicData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope is nil, want capability metadata")
	}
	if got, want := envelope.Truth.Capability, "platform_impact.change_surface"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", envelope.Data)
	}
	return data
}

// TestInvestigateChangeSurfaceAcceptsCodeTopicAndChangedPaths stays in
// package query although it exercises the impact change-surface handler: the
// topic half of the fixture is served by topicInvestigationContentStore,
// whose InvestigateCodeTopic method is typed on the lane-A code-topic family
// (codequery.CodeTopicEvidenceRow in code_topic.go), which an impact-package test
// cannot name without importing the query root back. The changed-paths half
// is covered from impact/ by
// TestInvestigateChangeSurfaceMapsChangedPathSymbolsPastRepoProbeWindow.
// See #6060.
func TestInvestigateChangeSurfaceAcceptsCodeTopicAndChangedPaths(t *testing.T) {
	t.Parallel()

	store := &topicInvestigationContentStore{
		fakePortContentStore: fakePortContentStore{
			entities: []EntityContent{
				{
					EntityID:     "entity-auth",
					EntityName:   "resolveGitHubAppAuth",
					EntityType:   "Function",
					RepoID:       "repo-1",
					RelativePath: "go/internal/collector/reposync/auth.go",
					Language:     "go",
					StartLine:    44,
					EndLine:      88,
				},
			},
		},
		rows: []codequery.CodeTopicEvidenceRow{
			{
				SourceKind:   "entity",
				RepoID:       "repo-1",
				RelativePath: "go/internal/collector/reposync/auth.go",
				EntityID:     "entity-auth",
				EntityName:   "resolveGitHubAppAuth",
				EntityType:   "Function",
				Language:     "go",
				StartLine:    44,
				EndLine:      88,
				MatchedTerms: []string{"repo", "sync", "auth"},
				Score:        3,
			},
		},
	}
	handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{"topic":"repo-sync auth behavior in the ingester","repo_id":"repo-1","changed_paths":["go/internal/collector/reposync/auth.go"],"limit":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface/investigate", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeChangeSurfaceCodeTopicData(t, w)
	codeSurface := data["code_surface"].(map[string]any)
	if got, want := int(codeSurface["matched_file_count"].(float64)), 1; got != want {
		t.Fatalf("matched_file_count = %d, want %d", got, want)
	}
	symbols := codeSurface["touched_symbols"].([]any)
	if got, want := len(symbols), 1; got != want {
		t.Fatalf("touched symbol count = %d, want %d", got, want)
	}
	nextCalls := data["recommended_next_calls"].([]any)
	if got, want := len(nextCalls), 2; got < want {
		t.Fatalf("recommended_next_calls = %d, want at least %d", got, want)
	}
}

func TestChangePlanningSearchesOnlyRequestedTopicWords(t *testing.T) {
	t.Parallel()

	for _, route := range []string{
		"/api/v0/impact/change-surface/investigate",
		"/api/v0/impact/pre-change",
		"/api/v0/impact/developer-change-plan",
	} {
		for _, tc := range []struct {
			topic string
			terms []string
		}{
			{topic: "showImage", terms: []string{"showimage"}},
			{topic: "change", terms: []string{"change"}},
			{topic: "surface", terms: []string{"surface"}},
		} {
			t.Run(route+"/"+tc.topic, func(t *testing.T) {
				t.Parallel()

				store := &topicInvestigationContentStore{}
				handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
				mux := http.NewServeMux()
				handler.Mount(mux)
				body, err := json.Marshal(map[string]any{
					"topic": tc.topic, "repo_id": "repo-1", "limit": 10,
					"changed_paths": []string{"src/topic.go"},
				})
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, route, bytes.NewReader(body))
				req.Header.Set("Accept", EnvelopeMIMEType)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
				}
				if len(store.requests) != 1 {
					t.Fatalf("topic reads = %d, want 1", len(store.requests))
				}
				got := store.requests[0]
				if got.Intent != "change_surface" {
					t.Fatalf("intent = %q, want change_surface", got.Intent)
				}
				if !slices.Equal(got.Terms, tc.terms) {
					t.Fatalf("terms = %#v, want %#v", got.Terms, tc.terms)
				}
			})
		}
	}
}

func TestInvestigateChangeSurfaceMarksCandidatePoolTruncation(t *testing.T) {
	t.Parallel()

	store := &topicInvestigationContentStore{rows: []codequery.CodeTopicEvidenceRow{{
		SourceKind:    "entity",
		RepoID:        "repo-1",
		RelativePath:  "src/change.go",
		EntityID:      "entity-change",
		EntityName:    "Change",
		MatchedTerms:  []string{"change"},
		Score:         1,
		PoolTruncated: true,
	}}}
	handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface/investigate",
		bytes.NewBufferString(`{"topic":"change","repo_id":"repo-1","limit":10}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	data := decodeChangeSurfaceCodeTopicData(t, w)
	if got := data["truncated"]; got != true {
		t.Fatalf("truncated = %#v, want true for capped candidate pool", got)
	}
	codeSurface := data["code_surface"].(map[string]any)
	if got := codeSurface["candidate_pool_truncated"]; got != true {
		t.Fatalf("code_surface.candidate_pool_truncated = %#v, want true", got)
	}
	metadata := data["answer_metadata"].(map[string]any)
	if got := metadata["truncated"]; got != true {
		t.Fatalf("answer_metadata.truncated = %#v, want true", got)
	}
}

func TestChangeSurfaceCodeBackendReportsPathLookupTruncation(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Content: &topicInvestigationContentStore{}, Profile: ProfileLocalAuthoritative}
	response, err := (changeSurfaceCodeBackend{}).FetchCodeSurface(
		context.Background(), handler,
		impact.ChangeSurfaceInvestigationRequest{
			RepoID: "repo-1", ChangedPaths: []string{"src/change.go"}, Limit: 10,
		},
		func(context.Context, impact.ChangeSurfaceInvestigationRequest) ([]map[string]any, bool, error) {
			return nil, true, nil
		},
	)
	if err != nil {
		t.Fatalf("FetchCodeSurface() error = %v", err)
	}
	coverage := response["coverage"].(map[string]any)
	if got := coverage["path_symbols_truncated"]; got != true {
		t.Fatalf("coverage.path_symbols_truncated = %#v, want true", got)
	}
	if got := response["truncated"]; got != true {
		t.Fatalf("truncated = %#v, want true", got)
	}
}

func TestInvestigateChangeSurfaceMarksEmptyOffsetPoolStatusUnknown(t *testing.T) {
	t.Parallel()

	store := &topicInvestigationContentStore{}
	handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface/investigate",
		bytes.NewBufferString(`{"topic":"change","repo_id":"repo-1","limit":10,"offset":10000}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	data := decodeChangeSurfaceCodeTopicData(t, w)
	codeSurface := data["code_surface"].(map[string]any)
	codeCoverage := codeSurface["coverage"].(map[string]any)
	if got := codeCoverage["candidate_pool_status"]; got != "unknown_empty_page" {
		t.Fatalf("code coverage candidate_pool_status = %#v, want unknown_empty_page", got)
	}
	coverage := data["coverage"].(map[string]any)
	if got := coverage["state"]; got != "partial" {
		t.Fatalf("coverage.state = %#v, want partial", got)
	}
	metadata := data["answer_metadata"].(map[string]any)
	if got := metadata["partial_reasons"].([]any); len(got) == 0 {
		t.Fatal("answer_metadata.partial_reasons is empty for unknown candidate pool status")
	}
}

// topicInvestigationContentStore serves canned code-topic rows to the
// change-surface handler without touching a backend. Twin of the same-named
// fake in codequery (code_topic_test.go): the InvestigateCodeTopic method is
// typed on the lane-A code-topic family (codequery.CodeTopicEvidenceRow),
// which a _test.go symbol in codequery cannot share across the package
// boundary (#6060).
type topicInvestigationContentStore struct {
	fakePortContentStore
	rows     []codequery.CodeTopicEvidenceRow
	requests []codequery.CodeTopicInvestigationRequest
	err      error
}

func (s *topicInvestigationContentStore) InvestigateCodeTopic(
	_ context.Context,
	req codequery.CodeTopicInvestigationRequest,
) ([]codequery.CodeTopicEvidenceRow, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	return append([]codequery.CodeTopicEvidenceRow(nil), s.rows...), nil
}

func TestChangePlanningCoveragePropagatesSpecificTruncationMarkers(t *testing.T) {
	t.Parallel()

	for _, route := range []string{
		"/api/v0/impact/change-surface/investigate",
		"/api/v0/impact/pre-change",
		"/api/v0/impact/developer-change-plan",
	} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()

			store := &topicInvestigationContentStore{
				fakePortContentStore: fakePortContentStore{entities: []EntityContent{
					{EntityID: "symbol-a", EntityName: "AuthA", EntityType: "Function", RepoID: "repo-1", RelativePath: "src/auth.go", Language: "go"},
					{EntityID: "symbol-b", EntityName: "AuthB", EntityType: "Function", RepoID: "repo-1", RelativePath: "src/auth.go", Language: "go"},
				}},
				rows: []codequery.CodeTopicEvidenceRow{{
					SourceKind:    "entity",
					RepoID:        "repo-1",
					RelativePath:  "src/auth.go",
					EntityID:      "symbol-a",
					EntityName:    "AuthA",
					EntityType:    "Function",
					Language:      "go",
					MatchedTerms:  []string{"auth"},
					Score:         1,
					PoolTruncated: true,
				}},
			}
			handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			body := `{"repo_id":"repo-1","topic":"auth","changed_paths":["src/auth.go"],"limit":1}`
			req := httptest.NewRequest(http.MethodPost, route, bytes.NewBufferString(body))
			req.Header.Set("Accept", EnvelopeMIMEType)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}

			var envelope querycontract.ResponseEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			data, ok := envelope.Data.(map[string]any)
			if !ok {
				t.Fatalf("data type = %T, want map[string]any", envelope.Data)
			}
			coverage, ok := data["coverage"].(map[string]any)
			if !ok {
				t.Fatalf("coverage type = %T, want map[string]any", data["coverage"])
			}
			for field, want := range map[string]any{
				"candidate_pool_truncated": true,
				"path_symbols_truncated":   true,
				"truncated":                true,
			} {
				if got := coverage[field]; got != want {
					t.Errorf("coverage.%s = %#v, want %#v", field, got, want)
				}
			}
			if route != "/api/v0/impact/change-surface/investigate" {
				packet, ok := data["answer_packet"].(map[string]any)
				if !ok {
					t.Fatalf("answer_packet type = %T, want map[string]any", data["answer_packet"])
				}
				if got := packet["partial"]; got != true {
					t.Errorf("answer_packet.partial = %#v, want true", got)
				}
			}
		})
	}
}
