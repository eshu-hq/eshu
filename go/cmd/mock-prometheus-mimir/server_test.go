// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHandlerReturnsBoundedQueueDepthMatrix(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	req := validRangeRequest(t, start)
	rec := httptest.NewRecorder()
	newHandler().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var body rangeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := body.Status, "success"; got != want {
		t.Fatalf("status body = %q, want %q", got, want)
	}
	if got, want := body.Data.ResultType, "matrix"; got != want {
		t.Fatalf("resultType = %q, want %q", got, want)
	}
	if got, want := len(body.Data.Result), 1; got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	values := body.Data.Result[0].Values
	if got, want := len(values), 2; got != want {
		t.Fatalf("sample count = %d, want %d", got, want)
	}
	if got, want := values[0][0], float64(start.Unix()); got != want {
		t.Fatalf("first timestamp = %#v, want %#v", got, want)
	}
	if got, want := values[0][1], "2"; got != want {
		t.Fatalf("first value = %#v, want %#v", got, want)
	}
	if got, want := values[1][1], "0"; got != want {
		t.Fatalf("second value = %#v, want %#v", got, want)
	}
}

func TestHandlerRejectsHostileRangeRequests(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		method string
		path   string
		mutate func(url.Values)
		want   int
	}{
		{name: "wrong method", method: http.MethodPost, path: rangeEndpoint, want: http.StatusMethodNotAllowed},
		{name: "wrong path", method: http.MethodGet, path: "/api/v1/query", want: http.StatusNotFound},
		{name: "wrong closed query", method: http.MethodGet, path: rangeEndpoint, mutate: func(q url.Values) { q.Set("query", "up") }, want: http.StatusBadRequest},
		{name: "wrong window", method: http.MethodGet, path: rangeEndpoint, mutate: func(q url.Values) { q.Set("end", start.Add(2*time.Hour).Format(time.RFC3339)) }, want: http.StatusBadRequest},
		{name: "wrong step", method: http.MethodGet, path: rangeEndpoint, mutate: func(q url.Values) { q.Set("step", "1s") }, want: http.StatusBadRequest},
		{name: "missing tenant", method: http.MethodGet, path: rangeEndpoint, mutate: func(url.Values) {}, want: http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			query := validRangeQuery(start)
			if tc.mutate != nil {
				tc.mutate(query)
			}
			req := httptest.NewRequest(tc.method, tc.path+"?"+query.Encode(), nil)
			if tc.name != "missing tenant" {
				req.Header.Set("X-Scope-OrgID", fixtureTenantID)
			}
			rec := httptest.NewRecorder()
			newHandler().ServeHTTP(rec, req)
			if got := rec.Code; got != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", got, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHealthIsStrict(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		method string
		want   int
	}{{method: http.MethodGet, want: http.StatusOK}, {method: http.MethodPost, want: http.StatusMethodNotAllowed}} {
		req := httptest.NewRequest(tc.method, healthEndpoint, nil)
		rec := httptest.NewRecorder()
		newHandler().ServeHTTP(rec, req)
		if got := rec.Code; got != tc.want {
			t.Fatalf("%s health status = %d, want %d", tc.method, got, tc.want)
		}
	}
}

func validRangeRequest(t *testing.T, start time.Time) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, rangeEndpoint+"?"+validRangeQuery(start).Encode(), nil)
	req.Header.Set("X-Scope-OrgID", fixtureTenantID)
	return req
}

func validRangeQuery(start time.Time) url.Values {
	return url.Values{
		"query": {queueDepthPromQL},
		"start": {start.Format(time.RFC3339)},
		"end":   {start.Add(time.Hour).Format(time.RFC3339)},
		"step":  {fixtureStep},
	}
}

func TestConfigFromEnvUsesPublicListenOverride(t *testing.T) {
	t.Parallel()

	cfg := configFromEnv(func(key string) string {
		if key == envListenAddr {
			return "127.0.0.1:29090"
		}
		return ""
	})
	if got, want := cfg.listenAddr, "127.0.0.1:29090"; got != want {
		t.Fatalf("listenAddr = %q, want %q", got, want)
	}
	if strings.Contains(cfg.listenAddr, "secret") {
		t.Fatalf("listenAddr contains secret-shaped text: %q", cfg.listenAddr)
	}
}
