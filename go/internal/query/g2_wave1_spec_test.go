// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// G2 Wave-1 seven-leg replication, leg 7: spec (Refs eshu-hq/eshu#9669,
// satellite eshu-hq/eshu-api#101). Core-side reading of the W8a spec
// leg: the core OWNS the fragments (unlike the dest-side disclosed
// absence), so the served openapi.json carries the wave's GET path
// keys with their pinned operationIds, carries NO post method under
// the 4 rider paths (a rider is a spelling, not a route — the
// disclosed absence), and stays 200 valid JSON. The wave-key list
// below is the disclosed set: 8 path keys for 9 rows, with the
// surface-inventory key shared by its GET base leg and its POST
// rider. Reference only — every line here is new core-side code.

// g2Wave1SpecKeys are the path keys the Wave-1 proof discloses: one
// entry per natively mounted wave path.
var g2Wave1SpecKeys = []string{
	"/api/v0/collector-extraction-readiness",
	"/api/v0/collector-extraction-readiness/{family}",
	"/api/v0/fact-schema-versions",
	"/api/v0/fact-schema-versions/{fact_kind}",
	"/api/v0/surface-inventory",
	"/api/v0/freshness/changed-since",
	"/api/v0/freshness/generations",
	"/api/v0/freshness/services/changed-since",
}

// g2Wave1SpecOperations pins the served method plus operationId per
// wave path key, mirroring the owning core fragments.
var g2Wave1SpecOperations = map[string]map[string]string{
	"/api/v0/collector-extraction-readiness":          {"get": "listCollectorExtractionReadiness"},
	"/api/v0/collector-extraction-readiness/{family}": {"get": "getCollectorExtractionReadiness"},
	"/api/v0/fact-schema-versions":                    {"get": "listFactSchemaVersions"},
	"/api/v0/fact-schema-versions/{fact_kind}":        {"get": "getFactSchemaVersion"},
	"/api/v0/surface-inventory":                       {"get": "listSurfaceInventory"},
	"/api/v0/freshness/changed-since":                 {"get": "summarizeChangedSince"},
	"/api/v0/freshness/generations":                   {"get": "listGenerationLifecycle"},
	"/api/v0/freshness/services/changed-since":        {"get": "summarizeServiceChangedSince"},
}

// g2Wave1ServedSpec fetches the served openapi.json as a decoded
// document.
func g2Wave1ServedSpec(t *testing.T) map[string]any {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/openapi.json", ServeOpenAPI)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/openapi.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("served spec status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Result().Body)
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("served spec is not valid JSON: %v", err)
	}
	return doc
}

// TestG2Wave1SpecKeyCount pins the disclosed set: 8 path keys for
// the 9 wave rows (the surface-inventory key is shared by its GET
// base leg and its POST rider spelling).
func TestG2Wave1SpecKeyCount(t *testing.T) {
	t.Parallel()

	if len(g2Wave1SpecKeys) != 8 {
		t.Fatalf("g2Wave1SpecKeys holds %d keys, want the 8 served wave path keys (one per mounted path)", len(g2Wave1SpecKeys))
	}
	rows, keys := len(g2Wave1), len(g2Wave1SpecKeys)
	if rows-keys != 1 {
		t.Fatalf("wave rows = %d, spec keys = %d, want exactly one shared key (the surface-inventory GET/POST twin)", rows, keys)
	}
	sorted := append([]string(nil), g2Wave1SpecKeys...)
	sort.Strings(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			t.Errorf("g2Wave1SpecKeys holds %q twice, want one entry per mounted path", sorted[i])
		}
	}
}

// TestG2Wave1NativeServesWaveSpecKeys pins that the served document
// carries every wave GET key with its pinned operationId.
func TestG2Wave1NativeServesWaveSpecKeys(t *testing.T) {
	t.Parallel()

	doc := g2Wave1ServedSpec(t)
	paths, _ := doc["paths"].(map[string]any)
	for _, key := range g2Wave1SpecKeys {
		item, ok := paths[key].(map[string]any)
		if !ok {
			t.Errorf("served spec lacks wave path key %s", key)
			continue
		}
		for method, wantID := range g2Wave1SpecOperations[key] {
			op, _ := item[method].(map[string]any)
			if got := op["operationId"]; got != wantID {
				t.Errorf("%s %s operationId = %v, want %q", key, method, got, wantID)
			}
		}
	}
}

// TestG2Wave1RiderPostsStayOutOfSpec pins the disclosed absence:
// no wave path key carries a post method — the 4 POST rider
// spellings are judgments, not routes, and promise nothing in the
// document.
func TestG2Wave1RiderPostsStayOutOfSpec(t *testing.T) {
	t.Parallel()

	doc := g2Wave1ServedSpec(t)
	paths, _ := doc["paths"].(map[string]any)
	for _, key := range g2Wave1SpecKeys {
		item, ok := paths[key].(map[string]any)
		if !ok {
			continue
		}
		if _, hasPost := item["post"]; hasPost {
			t.Errorf("served spec carries post under %s, want the rider spelling absent", key)
		}
	}
}
