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
