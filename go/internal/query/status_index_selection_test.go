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

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// selectionRecordingReader captures the selection requested through the filtered
// reader contract so handler tests can assert which sections a route loads.
type selectionRecordingReader struct {
	snapshot          statuspkg.RawSnapshot
	lastSelection     statuspkg.SnapshotSelection
	filteredCallCount int
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
	return r.snapshot, nil
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
