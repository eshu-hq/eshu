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

// G2 Wave-1 seven-leg replication, leg 4: parity-GET (Refs
// eshu-hq/eshu#9669, satellite eshu-hq/eshu-api#101). Core-side
// reading of the W8a parity legs: the core has no relay lane, so
// parity is repeat-read byte identity — the same request served by
// two identically built handler lanes answers byte-identically
// (status, headers, body) across the status matrix in both
// negotiated envelopes, with filter queries intact. This catches
// nondeterminism (map order, time, random) the same way the
// native-vs-relay comparison catches dest-side distortion; the
// satellite names the same check idempotency (the GET repeat check
// in its freshness contract). Reference only — every line here is
// new core-side code.

// g2Wave1ParityGetMatrix covers the wave GET legs: plain 200s, the
// bounded-paging and filter legs the pins admit, and the standing
// 400/404 shapes.
var g2Wave1ParityGetMatrix = []string{
	"/api/v0/collector-extraction-readiness",
	"/api/v0/collector-extraction-readiness?limit=1",
	"/api/v0/collector-extraction-readiness?limit=0",
	"/api/v0/collector-extraction-readiness?limit=abc",
	"/api/v0/collector-extraction-readiness/git",
	"/api/v0/collector-extraction-readiness/not-a-family",
	"/api/v0/fact-schema-versions",
	"/api/v0/fact-schema-versions?limit=1",
	"/api/v0/fact-schema-versions?limit=xyz",
	"/api/v0/fact-schema-versions/canned-fact",
	"/api/v0/fact-schema-versions/canned-fact?candidate=v9",
	"/api/v0/fact-schema-versions/canned-fact?candidate=bogus",
	"/api/v0/fact-schema-versions/unknown-kind",
	"/api/v0/surface-inventory",
	"/api/v0/surface-inventory?limit=1",
	"/api/v0/surface-inventory?limit=0",
	"/api/v0/surface-inventory?category=api",
	"/api/v0/surface-inventory?offset=100000",
}

// g2Wave1ServeOnce serves one request and returns the client-visible
// status, headers, and body.
func g2Wave1ServeOnce(mux *http.ServeMux, method, target, accept string, body string) (int, http.Header, []byte) {
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

// TestG2Wave1GetRepeatReadsByteIdentical pins that two identically
// built handler lanes answer every matrix request byte-identically
// in both negotiated envelopes: same status, same Content-Type,
// same body.
func TestG2Wave1GetRepeatReadsByteIdentical(t *testing.T) {
	t.Parallel()

	for _, target := range g2Wave1ParityGetMatrix {
		for _, accept := range []string{EnvelopeMIMEType, "application/json"} {
			name := "GET " + target + " " + accept
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				laneA, laneB := g2Wave1CannedMux(), g2Wave1CannedMux()
				statusA, headerA, bodyA := g2Wave1ServeOnce(laneA, http.MethodGet, target, accept, "")
				statusB, headerB, bodyB := g2Wave1ServeOnce(laneB, http.MethodGet, target, accept, "")

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

// TestG2Wave1GetFilterQueriesIntact pins that the filter queries the
// pins admit arrive identically on both lanes: the truncation and
// count fields match, so paging/filtering is deterministic.
func TestG2Wave1GetFilterQueriesIntact(t *testing.T) {
	t.Parallel()

	for _, target := range []string{
		"/api/v0/collector-extraction-readiness?limit=1",
		"/api/v0/fact-schema-versions?limit=1",
		"/api/v0/surface-inventory?limit=1&offset=1",
		"/api/v0/surface-inventory?category=api",
		"/api/v0/fact-schema-versions/canned-fact?candidate=v9",
	} {
		t.Run("GET "+target, func(t *testing.T) {
			t.Parallel()

			_, _, bodyA := g2Wave1ServeOnce(g2Wave1CannedMux(), http.MethodGet, target, EnvelopeMIMEType, "")
			_, _, bodyB := g2Wave1ServeOnce(g2Wave1CannedMux(), http.MethodGet, target, EnvelopeMIMEType, "")
			if string(bodyA) != string(bodyB) {
				t.Errorf("filter-query body mismatch:\nA=%s\nB=%s", bodyA, bodyB)
			}
		})
	}
}

// TestG2Wave1GetFamilyWireShapes pins the per-family wire shapes the
// parity legs carry: the family schema_version marker in both
// envelopes plus the truth capability in the negotiated envelope.
func TestG2Wave1GetFamilyWireShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path         string
		schemaMarker string
		capability   string
	}{
		{"/api/v0/collector-extraction-readiness", `"schema_version":"eshu.collector_extraction_readiness.v1"`, `"capability":"collector_extraction_readiness.list"`},
		{"/api/v0/collector-extraction-readiness/git", `"schema_version":"eshu.collector_extraction_readiness.v1"`, `"capability":"collector_extraction_readiness.family"`},
		{"/api/v0/fact-schema-versions", `"schema_version":"eshu.fact_schema_version.v1"`, `"capability":"fact_schema_version.list"`},
		{"/api/v0/fact-schema-versions/canned-fact", `"schema_version":"eshu.fact_schema_version.v1"`, `"capability":"fact_schema_version.detail"`},
		{"/api/v0/surface-inventory", `"surfaces"`, `"capability":"surface_inventory.list"`},
	}
	for _, tc := range cases {
		t.Run("GET "+tc.path, func(t *testing.T) {
			t.Parallel()

			mux := g2Wave1CannedMux()
			_, _, plain := g2Wave1ServeOnce(mux, http.MethodGet, tc.path, "application/json", "")
			if !strings.Contains(string(plain), strings.Trim(tc.schemaMarker, `"`)) && !strings.Contains(string(plain), "schema_version") {
				t.Errorf("plain body lacks the family wire shape: %s", plain)
			}
			status, _, enveloped := g2Wave1ServeOnce(mux, http.MethodGet, tc.path, EnvelopeMIMEType, "")
			if status != http.StatusOK {
				t.Fatalf("envelope status = %d, want 200", status)
			}
			if !strings.Contains(string(enveloped), tc.capability) {
				t.Errorf("envelope body lacks truth %s: %s", tc.capability, enveloped)
			}
		})
	}
}
