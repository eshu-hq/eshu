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

	legacy := request(t, "application/json")
	if _, ok := legacy["data"]; ok {
		t.Fatalf("legacy payload unexpectedly wrapped: %#v", legacy)
	}
	if !reflect.DeepEqual(legacy, data) {
		t.Fatalf("legacy payload differs from envelope data:\nlegacy = %#v\ndata = %#v", legacy, data)
	}
}
