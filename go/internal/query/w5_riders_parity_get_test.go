// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Wave-5 rider seven-leg replication, leg 4: parity-GET (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). Core-side
// reading of the W8a parity-GET legs: the rider judgments are byte
// identical across lanes and both envelopes, filter queries pass
// through intact (a twin with query params is still a twin), and the
// base legs keep their per-family wire shapes.

// w5RidersParityGetMatrix is the status matrix the parity legs prove:
// twin spellings with and without query params plus the R2/R3 bases,
// in both envelopes. Deeper subtrees (e.g. /api/v0/auth/providers/…)
// are not rows — no spec key exists for them on either side — and stay
// 404; the conformance leg pins that posture instead.
var w5RidersParityGetMatrix = []string{
	"/api/v0/auth/sign-in-policy/",
	"/api/v0/auth/sign-in-policy/?tenant_id=t1",
	"/api/v0/auth/sign-in-policy/?tenant_id=",
	"/api/v0/auth/providers/",
	"/api/v0/auth/providers/?tenant_id=t1",
	"/api/v0/auth/sign-in-policy",
	"/api/v0/auth/sign-in-policy?tenant_id=t1",
	"/api/v0/auth/providers?tenant_id=t1",
}

// w5RidersServeOnce serves one request and returns the client-visible
// status, headers, and body.
func w5RidersServeOnce(mux *http.ServeMux, method, target, accept, body string) (int, http.Header, []byte) {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", accept)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	res := rec.Result()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, data
}

// TestW5RidersGetJudgmentsByteIdentical pins that two identically
// built handler lanes judge every twin matrix request byte-identically
// in both negotiated envelopes: same status, same Content-Type, same
// body.
func TestW5RidersGetJudgmentsByteIdentical(t *testing.T) {
	t.Parallel()

	for _, target := range w5RidersParityGetMatrix {
		for _, accept := range []string{EnvelopeMIMEType, "application/json"} {
			name := "GET " + target + " " + accept
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				laneA, _, _ := w5RidersCannedMux()
				laneB, _, _ := w5RidersCannedMux()
				statusA, headerA, bodyA := w5RidersServeOnce(laneA, http.MethodGet, target, accept, "")
				statusB, headerB, bodyB := w5RidersServeOnce(laneB, http.MethodGet, target, accept, "")

				if statusA != statusB {
					t.Fatalf("lane status = %d vs %d, want identical", statusA, statusB)
				}
				if headerA.Get("Content-Type") != headerB.Get("Content-Type") {
					t.Errorf("Content-Type = %q vs %q, want identical", headerA.Get("Content-Type"), headerB.Get("Content-Type"))
				}
				if string(bodyA) != string(bodyB) {
					t.Errorf("lane body mismatch:\nA=%s\nB=%s", bodyA, bodyB)
				}
			})
		}
	}
}

// TestW5RidersTwinsStayTwins pins the never-split posture on the
// matrix: every slash-twin spelling 404s (never serves a base answer)
// while the R2/R3 bases serve, in both envelopes.
func TestW5RidersTwinsStayTwins(t *testing.T) {
	t.Parallel()

	mux, _, _ := w5RidersCannedMux()
	for _, target := range w5RidersParityGetMatrix {
		for _, accept := range []string{EnvelopeMIMEType, "application/json"} {
			t.Run("GET "+target+" "+accept, func(t *testing.T) {
				t.Parallel()

				status, _, _ := w5RidersServeOnce(mux, http.MethodGet, target, accept, "")
				twin := strings.HasSuffix(strings.Split(target, "?")[0], "/")
				if twin && status != http.StatusNotFound {
					t.Errorf("GET %s = %d, want 404 (a slash twin never serves)", target, status)
				}
				if !twin && status != http.StatusOK {
					t.Errorf("GET %s = %d, want 200 (the base serves through its pin)", target, status)
				}
			})
		}
	}
}

// TestW5RidersGetFamilyWireShapes pins the per-family wire shapes the
// parity legs carry: the sign-in-policy UI hint and the provider
// discovery envelope in both envelopes.
func TestW5RidersGetFamilyWireShapes(t *testing.T) {
	t.Parallel()

	mux, _, _ := w5RidersCannedMux()
	_, _, plain := w5RidersServeOnce(mux, http.MethodGet, "/api/v0/auth/sign-in-policy?tenant_id=t1", "application/json", "")
	if !strings.Contains(string(plain), `"require_sso"`) {
		t.Errorf("plain sign-in-policy body lacks the require_sso hint: %s", plain)
	}
	status, _, enveloped := w5RidersServeOnce(mux, http.MethodGet, "/api/v0/auth/providers?tenant_id=t1", EnvelopeMIMEType, "")
	if status != http.StatusOK {
		t.Fatalf("envelope providers status = %d, want 200", status)
	}
	if !strings.Contains(string(enveloped), `"provider_config_id":"canned"`) {
		t.Errorf("envelope providers body lacks the canned provider: %s", enveloped)
	}
}
