// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// readerFenceRouteCase drives one admin read route with a fenced reader
// error and asserts the retryable 503 carries the route's capability-matrix
// key (#8176). Before the fix each route answered 500 with the backend error
// text and no capability.
type readerFenceRouteCase struct {
	name       string
	capability string
	serve      func(store *stubAdminStore) *httptest.ResponseRecorder
}

func readerFenceErr() error {
	return fmt.Errorf("private read store: %w", db.ErrReaderStale)
}

func TestAdminReadReaderFenceCarriesCapability(t *testing.T) {
	t.Parallel()

	decisionsBody := map[string]any{
		"repository_id":    "repository:r_ok",
		"source_run_id":    "run-1",
		"include_evidence": true,
	}

	cases := []readerFenceRouteCase{
		{
			name:       "work-items query",
			capability: "operator.work_items.list",
			serve: func(store *stubAdminStore) *httptest.ResponseRecorder {
				store.workItemsErr = readerFenceErr()
				return postEnvelopeJSON(newAdminMux(&Handler{Store: store}),
					"/api/v0/admin/work-items/query",
					map[string]any{"statuses": []string{"failed"}})
			},
		},
		{
			name:       "decisions query",
			capability: "operator.decisions.list",
			serve: func(store *stubAdminStore) *httptest.ResponseRecorder {
				store.decisionsErr = readerFenceErr()
				return postEnvelopeJSON(newAdminMux(&Handler{Store: store}),
					"/api/v0/admin/decisions/query",
					map[string]any{"repository_id": "repository:r_ok", "source_run_id": "run-1"})
			},
		},
		{
			name:       "decision evidence read",
			capability: "operator.decision_evidence.list",
			serve: func(store *stubAdminStore) *httptest.ResponseRecorder {
				store.decisions = []DecisionRow{{
					DecisionID:   "d-1",
					DecisionType: "admit",
					RepositoryID: "repository:r_ok",
					SourceRunID:  "run-1",
					CreatedAt:    time.Now().UTC(),
				}}
				store.evidenceErr = readerFenceErr()
				return postEnvelopeJSON(newAdminMux(&Handler{Store: store}),
					"/api/v0/admin/decisions/query", decisionsBody)
			},
		},
		{
			name:       "replay-events query",
			capability: "operator.replay_events.list",
			serve: func(store *stubAdminStore) *httptest.ResponseRecorder {
				store.replayEvtErr = readerFenceErr()
				return postEnvelopeJSON(newAdminMux(&Handler{Store: store}),
					"/api/v0/admin/replay-events/query",
					map[string]any{})
			},
		},
		{
			name:       "repository-reindex catalog lookup",
			capability: "operator.repository_catalog.lookup",
			serve: func(_ *stubAdminStore) *httptest.ResponseRecorder {
				h := &Handler{
					Reindexer:           &stubReindexRequester{},
					Repositories:        stubRepositoryCatalog{err: readerFenceErr()},
					RepositoryReindexer: &stubRepositoryReindexer{},
				}
				req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/reindex",
					strings.NewReader(`{"scope":"repository","repositories":["payments"]}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
				w := httptest.NewRecorder()
				newAdminMux(h).ServeHTTP(w, req)
				return w
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := tc.serve(&stubAdminStore{})
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body: %s", w.Code, w.Body.String())
			}
			if got := w.Header().Get("Retry-After"); got == "" {
				t.Errorf("Retry-After header absent, want a retry hint on the fenced 503")
			}
			var envelope querycontract.ResponseEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode envelope: %v; body: %s", err, w.Body.String())
			}
			if envelope.Error == nil {
				t.Fatalf("envelope error absent; body: %s", w.Body.String())
			}
			if envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable {
				t.Errorf("envelope code = %q, want %q", envelope.Error.Code, querycontract.ErrorCodeBackendUnavailable)
			}
			if envelope.Error.Capability != tc.capability {
				t.Errorf("envelope capability = %q, want %q", envelope.Error.Capability, tc.capability)
			}
		})
	}
}

// postEnvelopeJSON mirrors postJSON but negotiates the Eshu error envelope,
// so a fenced-reader 503 carries its capability in the body.
func postEnvelopeJSON(mux *http.ServeMux, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// TestAdminReadNonFenceErrorKeepsStatus500 pins the unchanged behavior for a
// permanent store failure: #8176 only adds the fenced-reader 503 with a
// capability. Backend-text removal stays with the #7674 slice.
func TestAdminReadNonFenceErrorKeepsStatus500(t *testing.T) {
	t.Parallel()

	store := &stubAdminStore{workItemsErr: errors.New("postgres down")}
	w := postJSON(newAdminMux(&Handler{Store: store}),
		"/api/v0/admin/work-items/query",
		map[string]any{"statuses": []string{"failed"}})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
