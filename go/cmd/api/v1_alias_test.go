// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestV1Alias_RewritesPathAndDispatches(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"version":"v0","path":"` + r.URL.Path + `"}`))
	})
	mux.HandleFunc("POST /api/v0/submit", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		w.Write(body)
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mountV1Alias(mux)

	// GET /api/v1/test should resolve to the same handler as GET /api/v0/test
	t.Run("GET alias returns same handler", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/test status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"version":"v0"`) {
			t.Fatalf("GET /api/v1/test body = %q, want v0 handler response", rec.Body.String())
		}
		// The rewritten path that the handler sees should be /api/v0/test
		if !strings.Contains(rec.Body.String(), `"path":"/api/v0/test"`) {
			t.Fatalf("handler saw path %q, want /api/v0/test", rec.Body.String())
		}
	})

	// POST /api/v1/submit should resolve to POST /api/v0/submit
	t.Run("POST alias preserves method and body", func(t *testing.T) {
		body := strings.NewReader(`{"key":"value"}`)
		req := httptest.NewRequest("POST", "/api/v1/submit", body)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /api/v1/submit status = %d, want 201", rec.Code)
		}
		if rec.Body.String() != `{"key":"value"}` {
			t.Fatalf("POST /api/v1/submit body = %q, want %q", rec.Body.String(), `{"key":"value"}`)
		}
	})

	// /api/v0/ route still works
	t.Run("GET v0 still works", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v0/test", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v0/test status = %d, want 200", rec.Code)
		}
	})

	// /api/v1/ bare prefix returns 404 (no route at /api/v0/)
	t.Run("GET v1 bare prefix is 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /api/v1/ status = %d, want 404", rec.Code)
		}
	})

	// Non-/api/ paths are unaffected
	t.Run("GET health route unaffected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /health status = %d, want 200", rec.Code)
		}
	})
}

func TestV1Alias_PreservesQueryParameters(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/echo", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.RawQuery))
	})

	mountV1Alias(mux)

	req := httptest.NewRequest("GET", "/api/v1/echo?key=value&foo=bar", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "key=value&foo=bar" {
		t.Fatalf("query = %q, want 'key=value&foo=bar'", rec.Body.String())
	}
}

func TestV1Alias_PreservesHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/headers", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.Header.Get("X-Custom")))
	})

	mountV1Alias(mux)

	req := httptest.NewRequest("GET", "/api/v1/headers", nil)
	req.Header.Set("X-Custom", "hello-world")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "hello-world" {
		t.Fatalf("header value = %q, want 'hello-world'", rec.Body.String())
	}
}
