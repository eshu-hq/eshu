// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestChangePlanningCoverageIgnoresDeniedTopicRows(t *testing.T) {
	t.Parallel()

	routes := []string{
		"/api/v0/impact/change-surface/investigate",
		"/api/v0/impact/pre-change",
		"/api/v0/impact/developer-change-plan",
	}
	for _, route := range routes {
		for _, offset := range []int{0, 5} {
			t.Run(route+"/offset_"+strconv.Itoa(offset), func(t *testing.T) {
				t.Parallel()

				baseline := scopedChangePlanningResponse(t, route, nil, offset, []string{"repo-a"})
				for _, deniedPoolTruncated := range []bool{false, true} {
					hiddenOnly := []codequery.CodeTopicEvidenceRow{{
						SourceKind:    "entity",
						RepoID:        "repo-b",
						RelativePath:  "handlers/auth.go",
						EntityID:      "entity-b",
						EntityName:    "AuthenticateOther",
						PoolTruncated: deniedPoolTruncated,
					}}
					got := scopedChangePlanningResponse(t, route, hiddenOnly, offset, []string{"repo-a"})
					if !reflect.DeepEqual(got, baseline) {
						t.Errorf("hidden-only response differs from empty authorized result (denied PoolTruncated=%t):\n got: %#v\nwant: %#v", deniedPoolTruncated, got, baseline)
					}
				}

				authorizedOnly := []codequery.CodeTopicEvidenceRow{{
					SourceKind:   "entity",
					RepoID:       "repo-a",
					RelativePath: "handlers/auth.go",
					EntityID:     "entity-a",
					EntityName:   "Authenticate",
				}}
				authorizedResponse := scopedChangePlanningResponse(t, route, authorizedOnly, offset, []string{"repo-a"})
				for _, deniedPoolTruncated := range []bool{false, true} {
					withDenied := append(append([]codequery.CodeTopicEvidenceRow(nil), authorizedOnly...), codequery.CodeTopicEvidenceRow{
						SourceKind:    "entity",
						RepoID:        "repo-b",
						RelativePath:  "handlers/auth.go",
						EntityID:      "entity-b",
						EntityName:    "AuthenticateOther",
						PoolTruncated: deniedPoolTruncated,
					})
					got := scopedChangePlanningResponse(t, route, withDenied, offset, []string{"repo-a"})
					if !reflect.DeepEqual(got, authorizedResponse) {
						t.Errorf("authorized response changed after adding denied row (denied PoolTruncated=%t):\n got: %#v\nwant: %#v", deniedPoolTruncated, got, authorizedResponse)
					}
				}
			})
		}
	}
}

func TestChangePlanningEmptyScopedGrantSkipsContentRead(t *testing.T) {
	t.Parallel()

	for _, route := range []string{
		"/api/v0/impact/change-surface/investigate",
		"/api/v0/impact/pre-change",
		"/api/v0/impact/developer-change-plan",
	} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()

			store := &scopedCoverageContentStore{rows: []codequery.CodeTopicEvidenceRow{{
				SourceKind:    "entity",
				RepoID:        "repo-b",
				EntityID:      "entity-b",
				PoolTruncated: true,
			}}}
			handler := &ImpactHandler{Neo4j: graph.FakeGraphReaderWithSingle{}, Content: store, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodPost, route, bytes.NewBufferString(`{"topic":"auth","limit":10}`))
			req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
			req = req.WithContext(ContextWithAuthContext(req.Context(), testutil.ScopedTestAuthContext("tenant-a", nil)))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got := store.calls; got != 0 {
				t.Fatalf("content-store calls = %d, want no read for an empty scoped grant", got)
			}
		})
	}
}

func scopedChangePlanningResponse(
	t *testing.T,
	route string,
	rows []codequery.CodeTopicEvidenceRow,
	offset int,
	grants []string,
) map[string]any {
	t.Helper()

	store := &scopedCoverageContentStore{rows: rows}
	handler := &ImpactHandler{Neo4j: graph.FakeGraphReaderWithSingle{}, Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	body, err := json.Marshal(map[string]any{"topic": "auth", "limit": 10, "offset": offset})
	if err != nil {
		t.Fatalf("json.Marshal(request) error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, route, bytes.NewReader(body))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), testutil.ScopedTestAuthContext("tenant-a", grants)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("response data type = %T, want map[string]any", envelope.Data)
	}
	return data
}

type scopedCoverageContentStore struct {
	fakePortContentStore
	rows  []codequery.CodeTopicEvidenceRow
	calls int
}

func (s *scopedCoverageContentStore) InvestigateCodeTopic(
	_ context.Context,
	_ codequery.CodeTopicInvestigationRequest,
) ([]codequery.CodeTopicEvidenceRow, error) {
	s.calls++
	return append([]codequery.CodeTopicEvidenceRow(nil), s.rows...), nil
}
