// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIRouter_ServeSwaggerUIRoute(t *testing.T) {
	assertOpenAPIDocumentationRoute(t, "/api/v0/docs", "Swagger UI")
}

func TestAPIRouter_ServeReDocRoute(t *testing.T) {
	assertOpenAPIDocumentationRoute(t, "/api/v0/redoc", "ReDoc")
}

func assertOpenAPIDocumentationRoute(t *testing.T, path string, marker string) {
	t.Helper()

	router := &APIRouter{}
	mux := http.NewServeMux()
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d", path, w.Code, http.StatusOK)
	}
	if body := w.Body.String(); !strings.Contains(body, marker) {
		t.Fatalf("GET %s body missing %q", path, marker)
	}
}
