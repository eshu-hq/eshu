// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// Wave-5 rider seven-leg replication, leg 3: fail-closed (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). Core-side
// reading of the W8a fail-closed legs: the rider judgments land before
// any admission, store, or reader runs, so an unwired admission cannot
// fail the judgment open — and no wave leg synthesizes a Retry-After
// or a fail-open-to-empty answer.

// w5RidersNilMux mounts the rider handlers with nil admissions: the
// judgments must hold with zero contact on the same codes, proving
// the method judgment never depends on a wired store.
func w5RidersNilMux() *http.ServeMux {
	mux := http.NewServeMux()
	router := &APIRouter{
		Code:              &codequery.CodeHandler{},
		SignInPolicyReads: &SignInPolicyReadHandler{},
		AuthProviders:     &AuthProviderListHandler{},
	}
	router.Mount(mux)
	return mux
}

// TestW5RidersJudgeBeforeAdmission proves the judgments land before
// any admission is touched: with nil stores the R1 twin still 405s
// with Allow naming POST and the R2/R3 twins still 404.
func TestW5RidersJudgeBeforeAdmission(t *testing.T) {
	t.Parallel()

	mux := w5RidersNilMux()
	for _, entry := range w5Riders {
		t.Run(entry.Method+" "+entry.Path, func(t *testing.T) {
			t.Parallel()

			var req *http.Request
			if entry.Method == http.MethodPatch {
				req = httptest.NewRequest(http.MethodPatch, entry.Path, strings.NewReader(`{}`))
			} else {
				req = httptest.NewRequest(http.MethodGet, entry.Path, nil)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if entry.Method == http.MethodPatch {
				if rec.Code != http.StatusMethodNotAllowed {
					t.Fatalf("nil-store %s %s = %d, want 405 (judgment never depends on a wired store)", entry.Method, entry.Path, rec.Code)
				}
				if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodPost) {
					t.Errorf("nil-store %s %s: Allow = %q, want it to name the pinned POST method", entry.Method, entry.Path, allow)
				}
			} else if rec.Code != http.StatusNotFound {
				t.Fatalf("nil-store %s %s = %d, want 404 (a slash twin never serves)", entry.Method, entry.Path, rec.Code)
			}
			if retry := rec.Header().Get("Retry-After"); retry != "" {
				t.Errorf("nil-store %s %s sets Retry-After = %q, want none on a judged spelling", entry.Method, entry.Path, retry)
			}
		})
	}
}

// TestW5RidersNilStoresFailClosedNotOpen proves the base legs behind
// the riders fail closed — never fail-open-to-empty — when their
// admissions are unset: the sign-in-policy admin leg 503s with no
// Retry-After, the public sign-in-policy leg answers its safe default
// (the documented fail-open UI hint only, never a policy), and the
// providers leg answers its safe zero-configuration default.
func TestW5RidersNilStoresFailClosedNotOpen(t *testing.T) {
	t.Parallel()

	mux := w5RidersNilMux()
	serve := func(method, path string) (int, http.Header) {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+w5RidersTestKey)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Header()
	}
	if code, header := serve(http.MethodGet, "/api/v0/auth/admin/sign-in-policy"); code != http.StatusServiceUnavailable {
		t.Errorf("nil-store admin sign-in-policy = %d, want 503 (fail closed, never fail-open-to-empty)", code)
	} else if retry := header.Get("Retry-After"); retry != "" {
		t.Errorf("nil-store admin sign-in-policy sets Retry-After = %q, want none", retry)
	}
	if code, _ := serve(http.MethodGet, "/api/v0/auth/sign-in-policy"); code != http.StatusOK {
		t.Errorf("nil-store public sign-in-policy = %d, want the safe 200 default (documented UI-hint behavior)", code)
	}
	if code, _ := serve(http.MethodGet, "/api/v0/auth/providers"); code != http.StatusOK {
		t.Errorf("nil-store providers = %d, want the safe zero-configuration 200 default", code)
	}
}
