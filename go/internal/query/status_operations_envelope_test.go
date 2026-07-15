// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestGetOperationsNegotiatesEnvelopeAndPreservesLegacyRawJSON(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	request := func(t *testing.T, accept string) map[string]any {
		t.Helper()
		handler := &StatusHandler{
			StatusReader: fakeStatusReader{snapshot: statuspkg.RawSnapshot{AsOf: asOf}},
			LiveActivity: &fakeLiveActivityReader{},
		}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/v0/status/operations", nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
		}
		return payload
	}

	envelope := request(t, EnvelopeMIMEType)
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want operations object in negotiated envelope", envelope["data"])
	}
	if _, ok := data["live_activity"]; !ok {
		t.Fatalf("data = %#v, want live_activity", data)
	}
	if errorValue, ok := envelope["error"]; !ok || errorValue != nil {
		t.Fatalf("error = %#v, %t; want explicit null", errorValue, ok)
	}
	truth, ok := envelope["truth"].(map[string]any)
	if !ok {
		t.Fatalf("truth = %#v, want non-null operations truth object", envelope["truth"])
	}
	if got, want := truth["capability"], "operations.status"; got != want {
		t.Fatalf("truth.capability = %#v, want %#v", got, want)
	}
	if got, want := truth["level"], "exact"; got != want {
		t.Fatalf("truth.level = %#v, want %#v", got, want)
	}
	if got, want := truth["profile"], "production"; got != want {
		t.Fatalf("truth.profile = %#v, want %#v", got, want)
	}
	if got, want := truth["basis"], "runtime_state"; got != want {
		t.Fatalf("truth.basis = %#v, want %#v", got, want)
	}
	freshness, ok := truth["freshness"].(map[string]any)
	if !ok {
		t.Fatalf("truth.freshness = %#v, want object", truth["freshness"])
	}
	if got, want := freshness["state"], "fresh"; got != want {
		t.Fatalf("truth.freshness.state = %#v, want %#v", got, want)
	}
	if got, want := freshness["observed_at"], asOf.Format(time.RFC3339); got != want {
		t.Fatalf("truth.freshness.observed_at = %#v, want %#v", got, want)
	}

	legacy := request(t, "application/json")
	if _, ok := legacy["data"]; ok {
		t.Fatalf("legacy payload unexpectedly wrapped: %#v", legacy)
	}
	if !reflect.DeepEqual(legacy, data) {
		t.Fatalf("legacy payload differs from envelope data:\nlegacy = %#v\ndata = %#v", legacy, data)
	}
}
