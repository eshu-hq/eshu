// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBusinessRouteCheckpointSelection(t *testing.T) {
	mux := http.NewServeMux()
	for _, pattern := range []string{"GET /health", "GET /api/v0/openapi.json", "GET /api/v0/capabilities", "GET /api/v0/fact-schema-versions/{fact_kind}", "GET /api/v0/status/answer-narration", "GET /api/v0/status/pipeline", "GET /api/v0/auth/admin/audit/events", "GET /api/v0/auth/admin/roles", "POST /api/v0/auth/local/login", "POST /api/v0/ask", "GET /api/v0/future/business", "GET /api/v0/auth/future/business"} {
		mux.HandleFunc(pattern, func(http.ResponseWriter, *http.Request) {})
	}
	for _, test := range []struct {
		method, path        string
		pureNarration, want bool
	}{
		{"GET", "/health", false, false},
		{"GET", "/api/v0/openapi.json", false, false},
		{"GET", "/api/v0/capabilities", false, false},
		{"HEAD", "/api/v0/fact-schema-versions/code", false, false},
		{"GET", "/api/v0/status/answer-narration", true, false},
		{"GET", "/api/v0/status/answer-narration", false, true},
		{"GET", "/api/v0/status/pipeline", false, true},
		{"GET", "/api/v0/auth/admin/audit/events", false, true},
		{"GET", "/api/v0/auth/admin/roles", false, false},
		{"POST", "/api/v0/auth/local/login", false, false},
		{"POST", "/api/v0/ask", false, false},
		{"GET", "/api/v0/future/business", false, true},
		{"GET", "/api/v0/auth/future/business", false, true},
		{"GET", "/unknown", false, false},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		if got := RequiresCheckpoint(mux, request, test.pureNarration); got != test.want {
			t.Errorf("%s %s pure=%v: got %v want %v", test.method, test.path, test.pureNarration, got, test.want)
		}
	}
}
