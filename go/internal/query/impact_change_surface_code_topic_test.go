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

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
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
