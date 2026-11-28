// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// decodeJSONBody decodes a handler response body into a field map.
func decodeJSONBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return out
}

// postBQuery posts body to path on a mux carrying every B-query mount wired
// to working fake stores, proving each Mount* registers its route.
func TestMountBQueriesRegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	MountDeadLettersQuery(mux, &fakeDeadLetterStore{}, nil)
	MountDecisionsQuery(mux, &fakeDecisionStore{}, nil)
	MountInputInvalidFactsQuery(mux, &fakeInputInvalidStore{}, nil)
	MountPoisonedLinksQuery(mux, &fakePoisonedLinkStore{}, nil)
	MountReplayEventsQuery(mux, &fakeReplayEventStore{}, nil)
	for path, body := range map[string]string{
		"/api/v0/admin/dead-letters/query":                 `{"limit":1}`,
		"/api/v0/admin/decisions/query":                    `{"repository_id":"r","source_run_id":"s","limit":1}`,
		"/api/v0/admin/input-invalid-facts/query":          `{"scope_id":"s","generation_id":"g","limit":1}`,
		"/api/v0/admin/changed-since/poisoned-links/query": `{"limit":1}`,
		"/api/v0/admin/replay-events/query":                `{"limit":1}`,
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: want 200, got %d (%s)", path, rec.Code, rec.Body.String())
		}
	}
}

// cancelContext returns a request context that is already canceled, so a
// store error wrapping context.Canceled reads as a client cancel.
func cancelContext(t *testing.T, req *http.Request) *http.Request {
	t.Helper()
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	return req.WithContext(ctx)
}
