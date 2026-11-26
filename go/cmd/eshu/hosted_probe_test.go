// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHostedHealthProbeFallsBackOn404Only proves the hosted health seam
// probes GET /health first and retries the legacy GET /healthz only when
// /health answers 404 (an in-tree API surface behind the same endpoint).
// Any other /health outcome returns after the one request, and a double
// miss names both paths so the operator sees what was tried.
func TestHostedHealthProbeFallsBackOn404Only(t *testing.T) {
	tests := []struct {
		name         string
		healthCode   int
		healthzCode  int
		wantErr      bool
		wantPaths    []string
		wantErrParts []string
	}{
		{
			name:        "health 200, one request",
			healthCode:  http.StatusOK,
			healthzCode: http.StatusOK,
			wantErr:     false,
			wantPaths:   []string{"/health"},
		},
		{
			name:        "health 404 falls back to healthz 200",
			healthCode:  http.StatusNotFound,
			healthzCode: http.StatusOK,
			wantErr:     false,
			wantPaths:   []string{"/health", "/healthz"},
		},
		{
			name:         "double 404 names both paths",
			healthCode:   http.StatusNotFound,
			healthzCode:  http.StatusNotFound,
			wantErr:      true,
			wantPaths:    []string{"/health", "/healthz"},
			wantErrParts: []string{"/health", "/healthz"},
		},
		{
			name:        "health 401, no second request",
			healthCode:  http.StatusUnauthorized,
			healthzCode: http.StatusOK,
			wantErr:     true,
			wantPaths:   []string{"/health"},
		},
		{
			name:        "health 500, no fallback",
			healthCode:  http.StatusInternalServerError,
			healthzCode: http.StatusOK,
			wantErr:     true,
			wantPaths:   []string{"/health"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				switch r.URL.Path {
				case "/health":
					w.WriteHeader(tt.healthCode)
				case "/healthz":
					w.WriteHeader(tt.healthzCode)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)

			client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
			err := hostedHealthProbe(client)
			if (err != nil) != tt.wantErr {
				t.Fatalf("hostedHealthProbe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if strings.Join(paths, ",") != strings.Join(tt.wantPaths, ",") {
				t.Fatalf("requested paths = %v, want %v", paths, tt.wantPaths)
			}
			for _, part := range tt.wantErrParts {
				if err == nil || !strings.Contains(err.Error(), part) {
					t.Fatalf("hostedHealthProbe() error = %v, want it to name %q", err, part)
				}
			}
		})
	}
}
