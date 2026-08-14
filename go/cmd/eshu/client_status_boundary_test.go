// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/apierr"
)

// TestAPIHTTPErrorSatisfiesCLIStatusBoundary is the reason the accessor
// exists. apiHTTPError is unexported and lives in package main, so the
// internal/cli packages that classify transport errors cannot name it. This
// test drives the real APIClient against a real server, then hands the error
// it produced to apierr.StatusCode -- code compiled in a package that cannot
// import main -- and checks the status survives the crossing.
func TestAPIHTTPErrorSatisfiesCLIStatusBoundary(t *testing.T) {
	t.Parallel()

	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusNotImplemented,
		http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"denied"}`))
			}))
			defer server.Close()

			client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
			err := client.Get("/api/v0/health", nil)
			if err == nil {
				t.Fatalf("client.Get() error = nil, want an API error for status %d", status)
			}

			code, ok := apierr.StatusCode(err)
			if !ok {
				t.Fatalf("apierr.StatusCode(%v) ok = false, want true", err)
			}
			if code != status {
				t.Fatalf("apierr.StatusCode(%v) code = %d, want %d", err, code, status)
			}
		})
	}
}

// TestAPIHTTPErrorAccessorMatchesField pins the accessor to the field the five
// classification sites read, so the method and the struct cannot drift apart.
func TestAPIHTTPErrorAccessorMatchesField(t *testing.T) {
	t.Parallel()

	err := &apiHTTPError{StatusCode: http.StatusTeapot, Body: "short and stout"}
	if got := err.HTTPStatusCode(); got != err.StatusCode {
		t.Fatalf("HTTPStatusCode() = %d, want StatusCode field %d", got, err.StatusCode)
	}
}
