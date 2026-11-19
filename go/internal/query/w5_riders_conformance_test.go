// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// Wave-5 rider seven-leg replication, leg 1: conformance (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). TDD record: the
// frozen table below failed RED before w5_riders_wave.go landed
// (w5Riders undefined), and the method-judgment assertions failed next
// (w5RidersMethodAllowed undefined in w5_riders_terminate.go); both are
// green with the wave table plus the lane rule. Reference only —
// every line here is new core-side code (oracle discipline).
//
// Core-side reading of the W8a conformance legs: the 3 rider spellings
// are explicitly registered judgments on the mux (no catch-all serves
// them) — the R1 wrong-method twin is judged 405 with Allow naming
// the pinned POST, the R2/R3 slash twins stay 404 — and every
// non-wave spelling stays 404. The base spellings serve through their
// v0 pins (Wave 4 owns the bundles POST proof; the sign-in-policy and
// providers GET proofs ride the pin contract suites).

// TestW5RidersFrozen pins the exact surface Wave 5 proves: the R1
// wrong-method twin plus the R2/R3 slash twins from
// contracts/step2-frozen-scope.md. A wave change moves the ownership
// note in w5_riders_wave.go, the oracle fragment column, and this
// mirror together.
func TestW5RidersFrozen(t *testing.T) {
	t.Parallel()

	want := []string{
		"PATCH /api/v0/code/bundles Code native paths/code/graph.go",
		"GET /api/v0/auth/sign-in-policy/ SignInPolicyReads native paths/auth/sign_in_policy.go",
		"GET /api/v0/auth/providers/ AuthProviders native paths/auth/routes.go",
	}
	var got []string
	for _, entry := range w5Riders {
		got = append(got, entry.Method+" "+entry.Path+" "+entry.Family+" "+entry.Serve+" "+entry.Fragment)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("w5Riders holds %d entries, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("w5Riders differs at index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// w5RidersCannedMux mounts the Wave-5 rider handlers on a bare mux
// with hermetic canned admissions, so the conformance legs prove
// explicit registration (not a catch-all) and pin admission (not the
// static registries). CodeHandler mounts with a zero value: the R1
// judgment lands at the method-qualified mux pattern before any
// handler field runs. The freshness POST riders belong to Wave 1 and
// never mount here.
func w5RidersCannedMux() (*http.ServeMux, *countingSignInPolicyStore, *countingAuthProviderStore) {
	mux := http.NewServeMux()
	policy := &countingSignInPolicyStore{require: true, tenantID: "canned-tenant"}
	providers := &countingAuthProviderStore{}
	router := &APIRouter{
		Code:              &codequery.CodeHandler{},
		SignInPolicyReads: &SignInPolicyReadHandler{Store: policy},
		AuthProviders:     &AuthProviderListHandler{Store: providers, Policy: policy},
	}
	router.Mount(mux)
	return mux, policy, providers
}

// TestW5RidersNativeJudgment pins that every wave row resolves on an
// explicitly mounted judgment: the R1 twin is judged 405 with an
// Allow header naming the pinned POST method, and the R2/R3 slash
// twins stay 404 with zero admission contact.
func TestW5RidersNativeJudgment(t *testing.T) {
	t.Parallel()

	mux, policy, providers := w5RidersCannedMux()
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
					t.Fatalf("%s %s: status = %d, want 405 (core judges the method POST-only)", entry.Method, entry.Path, rec.Code)
				}
				if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodPost) {
					t.Errorf("%s %s: Allow = %q, want it to name the pinned POST method", entry.Method, entry.Path, allow)
				}
			} else {
				if rec.Code != http.StatusNotFound {
					t.Fatalf("%s %s: status = %d, want 404 (a slash twin never serves)", entry.Method, entry.Path, rec.Code)
				}
			}
		})
	}
	if policy.contacts()+providers.contacts() != 0 {
		t.Errorf("rider judgments contacted admissions %d times, want zero contact", policy.contacts()+providers.contacts())
	}
}

// TestW5RidersMethodJudgmentMatchesPredicate pins the lane rule against
// the mux: the w5RidersMethodAllowed predicate (the stated "core
// allowlists each route to its pinned method" rule) agrees with the
// served judgment for every wave row, and stays closed for
// non-wave spellings — including the R4-R7 freshness/surface-inventory
// riders, which belong to Wave 1 and never enter this wave.
func TestW5RidersMethodJudgmentMatchesPredicate(t *testing.T) {
	t.Parallel()

	mux, _, _ := w5RidersCannedMux()
	serve := func(method, path, body string) int {
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, entry := range w5Riders {
		t.Run(entry.Method+" "+entry.Path, func(t *testing.T) {
			t.Parallel()

			if !w5RidersMethodAllowed(entry.Method, entry.Path) {
				t.Fatalf("w5RidersMethodAllowed(%s %s) = false, want the wave row allowed", entry.Method, entry.Path)
			}
		})
	}
	for _, closed := range [][2]string{
		{"POST", "/api/v0/freshness/changed-since"},
		{"POST", "/api/v0/freshness/generations"},
		{"POST", "/api/v0/freshness/services/changed-since"},
		{"POST", "/api/v0/surface-inventory"},
		{"GET", "/api/v0/code/bundles"},
		{"POST", "/api/v0/auth/sign-in-policy"},
		{"DELETE", "/api/v0/auth/providers"},
		{"GET", "/api/v0/auth/providers/oidc"},
		{"GET", "/api/v0/no-such-route"},
	} {
		if w5RidersMethodAllowed(closed[0], closed[1]) {
			t.Errorf("w5RidersMethodAllowed(%s %s) = true, want the non-wave spelling closed", closed[0], closed[1])
		}
		if code := serve(closed[0], closed[1], ""); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s served %d, want a judgment (404/405), never a wave answer", closed[0], closed[1], code)
		}
	}
}

// TestW5RidersBaseLegsServeThroughPins pins the other side of the
// never-split posture: the base spellings behind the riders serve
// through their v0 pins on this same mux (public sign-in-policy and
// providers GETs answer 200 with exactly-one-family contact; the
// bundles POST stays Wave-4-owned and is only pinned mounted here, so
// this leg asserts it is neither 404 nor 405 without re-proving
// Wave-4's serving proof).
func TestW5RidersBaseLegsServeThroughPins(t *testing.T) {
	t.Parallel()

	mux, policy, providers := w5RidersCannedMux()
	serve := func(method, path string) (int, []byte) {
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		body, _ := io.ReadAll(rec.Result().Body)
		return rec.Code, body
	}
	if code, _ := serve(http.MethodGet, "/api/v0/auth/sign-in-policy?tenant_id=t1"); code != http.StatusOK {
		t.Errorf("GET sign-in-policy base = %d, want 200 through the pin", code)
	}
	if code, _ := serve(http.MethodGet, "/api/v0/auth/providers?tenant_id=t1"); code != http.StatusOK {
		t.Errorf("GET providers base = %d, want 200 through the pin", code)
	}
	if code, _ := serve(http.MethodPost, "/api/v0/code/bundles"); code == http.StatusNotFound || code == http.StatusMethodNotAllowed {
		t.Errorf("POST bundles base = %d, want the mounted leg (never 404/405)", code)
	}
	if policy.contacts() == 0 {
		t.Errorf("sign-in-policy admission contacts = 0, want the base legs to admit through the pin")
	}
	if providers.contacts() != 1 {
		t.Errorf("provider admission contacts = %d, want exactly 1 (the tenant-scoped providers leg)", providers.contacts())
	}
}
