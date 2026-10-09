// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// storedActiveWorkSnapshot is a RawSnapshot whose active-work sections came
// from a stored summary row that passed every fence.
func storedActiveWorkSnapshot() statuspkg.RawSnapshot {
	asOf := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	return statuspkg.RawSnapshot{
		AsOf: asOf,
		ActiveWorkSource: statuspkg.ActiveWorkSource{
			Source: statuspkg.ActiveWorkSourceModel, Reason: statuspkg.ActiveWorkReasonFresh,
			AsOf: asOf, Age: 12 * time.Second,
		},
	}
}

// liveFallbackActiveWorkSnapshot is a RawSnapshot whose stored row could not
// be served, so the live statement answered.
func liveFallbackActiveWorkSnapshot() statuspkg.RawSnapshot {
	asOf := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	return statuspkg.RawSnapshot{
		AsOf: asOf,
		ActiveWorkSource: statuspkg.ActiveWorkSource{
			Source: statuspkg.ActiveWorkSourceLiveFallback, Reason: statuspkg.ActiveWorkReasonStale,
			AsOf: asOf,
		},
	}
}

func requireActiveWorkSource(t *testing.T, path string, payload map[string]any, wantSource, wantReason string, wantAge float64) {
	t.Helper()
	source, ok := payload["active_work_source"].(map[string]any)
	if !ok {
		t.Fatalf("GET %s has no active_work_source object: %#v", path, payload)
	}
	if source["source"] != wantSource || source["reason"] != wantReason ||
		source["as_of"] != "2026-10-08T12:00:00Z" || source["age_seconds"] != wantAge ||
		source["stale"] != false {
		t.Fatalf("GET %s active_work_source = %#v", path, source)
	}
}

// TestFreshnessCausalityAndControlPlaneCarryTheActiveWorkSource proves the two
// status routes #7660 names (freshness-causality directly, operator control
// plane via triage) carry the marker for a stored read and a live fallback.
func TestFreshnessCausalityAndControlPlaneCarryTheActiveWorkSource(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/status/freshness-causality",
		"/api/v0/status/operator-control-plane",
	} {
		t.Run(path+"/stored", func(t *testing.T) {
			t.Parallel()
			requireActiveWorkSource(t, path, getStatusPayload(t, storedActiveWorkSnapshot(), path), "model", "fresh", float64(12))
		})
		t.Run(path+"/live_fallback", func(t *testing.T) {
			t.Parallel()
			requireActiveWorkSource(t, path, getStatusPayload(t, liveFallbackActiveWorkSnapshot(), path), "live_fallback", "stale", float64(0))
		})
	}
}

func TestFreshnessCausalityAndControlPlaneOmitTheActiveWorkSourceWhenTheReaderReportsNone(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/status/freshness-causality",
		"/api/v0/status/operator-control-plane",
	} {
		payload := getStatusPayload(t, statuspkg.RawSnapshot{AsOf: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}, path)
		if _, present := payload["active_work_source"]; present {
			t.Fatalf("GET %s emitted active_work_source for a reader that reports none", path)
		}
	}
}

func getLiveEvidenceBundlePayload(t *testing.T, snapshot statuspkg.RawSnapshot) map[string]any {
	t.Helper()
	handler := &EvidenceHandler{
		StatusReader: fakeStatusReader{snapshot: snapshot},
		Neo4j:        evidenceBundleFixtureGraph{count: 5},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v0/evidence/bundle status = %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("GET /api/v0/evidence/bundle: %v", err)
	}
	return payload
}

// TestLiveEvidenceBundleCarriesTheActiveWorkSource proves the live bundle
// carries the marker for a stored read and a live fallback, and omits it
// when the reader reports none.
func TestLiveEvidenceBundleCarriesTheActiveWorkSource(t *testing.T) {
	t.Parallel()

	stored := evidenceBundleFixtureSnapshot()
	stored.ActiveWorkSource = storedActiveWorkSnapshot().ActiveWorkSource
	requireActiveWorkSource(t, "/api/v0/evidence/bundle", getLiveEvidenceBundlePayload(t, stored), "model", "fresh", float64(12))

	fallback := evidenceBundleFixtureSnapshot()
	fallback.ActiveWorkSource = liveFallbackActiveWorkSnapshot().ActiveWorkSource
	requireActiveWorkSource(t, "/api/v0/evidence/bundle", getLiveEvidenceBundlePayload(t, fallback), "live_fallback", "stale", float64(0))

	omitted := getLiveEvidenceBundlePayload(t, evidenceBundleFixtureSnapshot())
	if _, present := omitted["active_work_source"]; present {
		t.Fatalf("GET /api/v0/evidence/bundle emitted active_work_source for a reader that reports none")
	}
}

// TestOpenAPIDocumentsTheActiveWorkSourceOnThe7660Routes proves the three
// routes carry the shared ActiveWorkSource component reference.
func TestOpenAPIDocumentsTheActiveWorkSourceOnThe7660Routes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	for _, path := range []string{
		"/api/v0/evidence/bundle",
		"/api/v0/status/freshness-causality",
		"/api/v0/status/operator-control-plane",
	} {
		route := testutil.MustMapField(t, paths, path)
		get := testutil.MustMapField(t, route, "get")
		responses := testutil.MustMapField(t, get, "responses")
		okResp := testutil.MustMapField(t, responses, "200")
		content := testutil.MustMapField(t, okResp, "content")
		jsonBody := testutil.MustMapField(t, content, "application/json")
		schema := testutil.MustMapField(t, jsonBody, "schema")
		properties := testutil.MustMapField(t, schema, "properties")
		marker, ok := properties["active_work_source"].(map[string]any)
		if !ok {
			t.Fatalf("%s response schema missing active_work_source", path)
		}
		if marker["$ref"] != "#/components/schemas/ActiveWorkSource" {
			t.Fatalf("%s active_work_source = %#v, want the shared component ref", path, marker)
		}
	}
}
