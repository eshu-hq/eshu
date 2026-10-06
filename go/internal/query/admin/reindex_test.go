// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func postRawReindex(h *Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/reindex", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	newAdminMux(h).ServeHTTP(w, req)
	return w
}

// TestAdminHandler_ReindexAcceptsWatermarkRequest pins the #7620 202 body: the
// stored watermark comes back as requested_at and the detail states what the
// ingesters do with it. An empty object takes the defaults.
func TestAdminHandler_ReindexAcceptsWatermarkRequest(t *testing.T) {
	t.Parallel()

	stored := time.Date(2026, 10, 5, 8, 0, 0, 123000, time.FixedZone("EDT", -4*3600))
	for _, body := range []string{
		`{"ingester":"repository","scope":"workspace","force":true}`,
		`{}`,
	} {
		requester := &stubReindexRequester{requestedAt: stored}
		w := postRawReindex(&Handler{Reindexer: requester}, body)
		if w.Code != http.StatusAccepted {
			t.Fatalf("body %s: status = %d, want 202; body: %s", body, w.Code, w.Body.String())
		}
		got := decodeBody(t, w)
		want := map[string]any{
			"status":       "accepted",
			"ingester":     "repository",
			"scope":        "workspace",
			"force":        true,
			"requested_at": "2026-10-05T12:00:00.000123Z",
		}
		for key, value := range want {
			if got[key] != value {
				t.Errorf("body %s: %s = %v, want %v", body, key, got[key], value)
			}
		}
		detail, _ := got["detail"].(string)
		for _, phrase := range []string{"requested_at", "full re-parse", "activated full generation"} {
			if !strings.Contains(detail, phrase) {
				t.Errorf("detail %q lacks %q", detail, phrase)
			}
		}
		if len(requester.ingesters) != 1 || requester.ingesters[0] != "repository" {
			t.Fatalf("body %s: RequestReindex ingesters = %v, want [repository]", body, requester.ingesters)
		}
	}
}

// TestAdminHandler_ReindexRejectsUnsupportedRequests: the route only records a
// fleet-wide repository reindex. Any other ingester, a narrower scope, a
// non-forcing request, or the retired workspace path/action fields are
// rejected before anything is stored, instead of a 202 that does nothing.
func TestAdminHandler_ReindexRejectsUnsupportedRequests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown ingester", `{"ingester":"terraform"}`, `ingester must be "repository"`},
		{"narrow scope", `{"scope":"repository"}`, "per-repository reindex is not supported"},
		{"force false", `{"force":false}`, "force"},
		{"workspace path", `{"scope":"workspace","path":"/src/app"}`, `unknown field "path"`},
		{"workspace action", `{"scope":"workspace","action":"sync"}`, `unknown field "action"`},
		{"empty body", ``, "invalid JSON"},
		{"trailing value", `{} {"ingester":"terraform"}`, "unexpected trailing data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			requester := &stubReindexRequester{}
			w := postRawReindex(&Handler{Reindexer: requester}, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
			if detail, _ := decodeBody(t, w)["detail"].(string); !strings.Contains(detail, tc.want) {
				t.Fatalf("detail = %q, want it to mention %q", detail, tc.want)
			}
			if len(requester.ingesters) != 0 {
				t.Fatalf("RequestReindex called %d times, want 0 for a rejected request", len(requester.ingesters))
			}
		})
	}
}

// TestAdminHandler_ReindexStoreFailure surfaces a failed write as 500 and an
// unwired requester as 503.
func TestAdminHandler_ReindexStoreFailure(t *testing.T) {
	t.Parallel()

	w := postRawReindex(&Handler{Reindexer: &stubReindexRequester{err: errors.New("postgres down")}}, `{}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("store failure status = %d, want 500", w.Code)
	}
	w = postRawReindex(&Handler{}, `{}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired status = %d, want 503", w.Code)
	}
}
