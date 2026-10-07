// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// selectionRecordingReader captures the selection requested through the filtered
// reader contract so handler tests can assert which sections a route loads.
type selectionRecordingReader struct {
	snapshot              statuspkg.RawSnapshot
	omitTerraformOnSkip   bool
	returnedTerraformRows int
	lastSelection         statuspkg.SnapshotSelection
	filteredCallCount     int
}

func (r *selectionRecordingReader) ReadStatusSnapshot(
	ctx context.Context,
	asOf time.Time,
) (statuspkg.RawSnapshot, error) {
	return r.ReadStatusSnapshotFiltered(ctx, asOf, statuspkg.FullSnapshotSelection())
}

func (r *selectionRecordingReader) ReadStatusSnapshotFiltered(
	_ context.Context,
	_ time.Time,
	selection statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	r.lastSelection = selection
	r.filteredCallCount++
	raw := r.snapshot
	if r.omitTerraformOnSkip && selection.SkipTerraformStateEvidence {
		raw.TerraformStateLastSerials = nil
		raw.TerraformStateRecentWarnings = nil
	}
	r.returnedTerraformRows = len(raw.TerraformStateLastSerials) + len(raw.TerraformStateRecentWarnings)
	return raw, nil
}

func TestGetIndexStatusRequestsFilteredSelection(t *testing.T) {
	t.Parallel()

	reader := &selectionRecordingReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
		},
	}
	handler := &StatusHandler{StatusReader: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/index", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if reader.filteredCallCount != 1 {
		t.Fatalf("filtered reader call count = %d, want 1", reader.filteredCallCount)
	}
	if reader.lastSelection.IncludeCollectorFactEvidence {
		t.Fatalf("index status requested collector fact evidence; want excluded")
	}
	if reader.lastSelection.IncludeRegistryCollectors {
		t.Fatalf("index status requested registry collectors; want excluded")
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	for _, key := range []string{"queue", "coordinator", "repository_count"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("index status payload missing %q field: %#v", key, payload)
		}
	}
}

func TestGetPipelineStatusRequestsFullSelection(t *testing.T) {
	t.Parallel()

	reader := &selectionRecordingReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
		},
	}
	handler := &StatusHandler{StatusReader: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/pipeline", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if !reader.lastSelection.IncludeCollectorFactEvidence {
		t.Fatalf("pipeline status excluded collector fact evidence; want included")
	}
	if !reader.lastSelection.IncludeRegistryCollectors {
		t.Fatalf("pipeline status excluded registry collectors; want included")
	}
}

func TestGetSemanticExtractionStatusRequestsSemanticOnlySelection(t *testing.T) {
	t.Parallel()

	reader := &selectionRecordingReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
			SemanticExtraction: statuspkg.SemanticExtractionStatus{
				State:              statuspkg.SemanticExtractionAvailable,
				ProviderConfigured: true,
			},
		},
	}
	handler := &StatusHandler{StatusReader: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/semantic-extraction", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if reader.filteredCallCount != 1 || reader.lastSelection != statuspkg.SemanticOnlySnapshotSelection() {
		t.Fatalf("selection = %+v, calls = %d; want semantic-only filtered read", reader.lastSelection, reader.filteredCallCount)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if got := envelope.Data.(map[string]any)["state"]; got != statuspkg.SemanticExtractionAvailable {
		t.Fatalf("state = %v, want available", got)
	}
	if envelope.Truth == nil || envelope.Truth.Freshness.State != querycontract.FreshnessFresh {
		t.Fatalf("truth = %+v, want current", envelope.Truth)
	}
}

// TestIngesterRoutesSkipTerraformWhileIndexAndPipelineKeepIt pins that the
// ingester list and detail routes omit the Terraform-state reads they never
// render (#7009) while index and pipeline status keep them.
func TestIngesterRoutesSkipTerraformWhileIndexAndPipelineKeepIt(t *testing.T) {
	reader := &selectionRecordingReader{snapshot: statuspkg.RawSnapshot{AsOf: time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)}}
	h := &StatusHandler{StatusReader: reader}
	mux := http.NewServeMux()
	h.Mount(mux)
	for _, route := range []string{"/api/v0/status/ingesters/repository", "/api/v0/ingesters/repository", "/api/v0/status/ingesters", "/api/v0/ingesters"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route, nil))
		if rec.Code != http.StatusOK || !reader.lastSelection.SkipTerraformStateEvidence {
			t.Fatalf("ingester %s: status=%d selection=%+v", route, rec.Code, reader.lastSelection)
		}
	}
	for _, route := range []string{"/api/v0/status/index", "/api/v0/index-status", "/api/v0/status/pipeline"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route, nil))
		if rec.Code != http.StatusOK || reader.lastSelection.SkipTerraformStateEvidence {
			t.Fatalf("Terraform-rendering %s: status=%d selection=%+v", route, rec.Code, reader.lastSelection)
		}
	}
}

func sourcedSnapshot() statuspkg.RawSnapshot {
	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	return statuspkg.RawSnapshot{
		AsOf: asOf,
		ActiveWorkSource: statuspkg.ActiveWorkSource{
			Source: statuspkg.ActiveWorkSourceLiveFallback, Reason: statuspkg.ActiveWorkReasonStale,
			AsOf: asOf,
		},
	}
}

func getStatusPayload(t *testing.T, snapshot statuspkg.RawSnapshot, path string) map[string]any {
	t.Helper()
	handler := &StatusHandler{StatusReader: &selectionRecordingReader{snapshot: snapshot}}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d: %s", path, rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return payload
}

func TestStatusRoutesCarryTheActiveWorkSource(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/status/pipeline",
		"/api/v0/status/index",
		"/api/v0/ingesters/repository",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			payload := getStatusPayload(t, sourcedSnapshot(), path)
			source, ok := payload["active_work_source"].(map[string]any)
			if !ok {
				t.Fatalf("GET %s has no active_work_source object: %#v", path, payload)
			}
			if source["source"] != "live_fallback" || source["reason"] != "stale" || source["stale"] != false ||
				source["as_of"] != "2026-10-06T12:00:00Z" || source["age_seconds"] != float64(0) {
				t.Fatalf("GET %s active_work_source = %#v", path, source)
			}
		})
	}
}

func TestStatusRoutesOmitTheActiveWorkSourceWhenTheReaderReportsNone(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/status/pipeline",
		"/api/v0/status/index",
		"/api/v0/ingesters/repository",
	} {
		payload := getStatusPayload(t, statuspkg.RawSnapshot{AsOf: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}, path)
		if _, present := payload["active_work_source"]; present {
			t.Fatalf("GET %s emitted active_work_source for a reader that reports none", path)
		}
	}
}

func TestIngesterListCarriesTheActiveWorkSource(t *testing.T) {
	t.Parallel()

	payload := getStatusPayload(t, sourcedSnapshot(), "/api/v0/ingesters")
	if _, ok := payload["active_work_source"].(map[string]any); !ok {
		t.Fatalf("GET /api/v0/ingesters has no active_work_source object: %#v", payload)
	}
}

func TestHostedReadinessAndOperationsCarryTheActiveWorkSource(t *testing.T) {
	t.Parallel()

	snapshot := sourcedSnapshot()
	report := statuspkg.BuildReport(snapshot, statuspkg.DefaultOptions())

	readiness, err := json.Marshal(buildHostedReadinessReport(snapshot, report, 1, nil, false))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(readiness, &decoded); err != nil {
		t.Fatal(err)
	}
	if source, ok := decoded["active_work_source"].(map[string]any); !ok || source["reason"] != "stale" {
		t.Fatalf("hosted readiness active_work_source = %#v", decoded["active_work_source"])
	}

	ops := statuspkg.Operations(report, nil, false, 10)
	if source, ok := operationsToMap(ops, false)["active_work_source"].(*statuspkg.ActiveWorkSourceJSON); !ok || source.Reason != "stale" {
		t.Fatalf("operations active_work_source = %#v", operationsToMap(ops, false)["active_work_source"])
	}
	if _, present := operationsToMap(ops, true)["active_work_source"]; present {
		t.Fatal("a scoped operations response, which withholds the queue sections, still carries active_work_source")
	}
}
