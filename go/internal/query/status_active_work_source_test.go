// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

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
