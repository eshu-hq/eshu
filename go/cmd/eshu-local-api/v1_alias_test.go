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

	handler := v1PrefixAliasMiddleware(mux)

	t.Run("GET alias returns same handler", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/test status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"version":"v0"`) {
			t.Fatalf("GET /api/v1/test body = %q, want v0 handler response", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"path":"/api/v0/test"`) {
			t.Fatalf("handler saw path %q, want /api/v0/test", rec.Body.String())
		}
	})

	t.Run("POST alias preserves method and body", func(t *testing.T) {
		body := strings.NewReader(`{"key":"value"}`)
		req := httptest.NewRequest("POST", "/api/v1/submit", body)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /api/v1/submit status = %d, want 201", rec.Code)
		}
		if rec.Body.String() != `{"key":"value"}` {
			t.Fatalf("POST /api/v1/submit body = %q, want %q", rec.Body.String(), `{"key":"value"}`)
		}
	})

	t.Run("GET v0 still works", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v0/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v0/test status = %d, want 200", rec.Code)
		}
	})

	t.Run("GET v1 bare prefix is 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /api/v1/ status = %d, want 404", rec.Code)
		}
	})

	t.Run("GET health route unaffected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

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

	handler := v1PrefixAliasMiddleware(mux)

	req := httptest.NewRequest("GET", "/api/v1/echo?key=value&foo=bar", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

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

	handler := v1PrefixAliasMiddleware(mux)

	req := httptest.NewRequest("GET", "/api/v1/headers", nil)
	req.Header.Set("X-Custom", "hello-world")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "hello-world" {
		t.Fatalf("header value = %q, want 'hello-world'", rec.Body.String())
	}
}

// TestV1Alias_MiddlewareRewritesBeforeAuth verifies the v1 prefix alias is
// applied as middleware so that downstream handlers (auth, scoped-token
// classification) see the rewritten /api/v0/ path, not the raw /api/v1/
// path. This simulates the layering order: v1PrefixAliasMiddleware before
// the auth gate.
func TestV1Alias_MiddlewareRewritesBeforeAuth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.Path))
	})

	// Simulate the auth gate by wrapping the middleware output with a
	// path checker that must see /api/v0/ for the request to proceed.
	authGate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/v0/") {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// Must be: v1PrefixAliasMiddleware wraps authGate so v1 paths
	// get rewritten before the gate sees them.
	handler := v1PrefixAliasMiddleware(authGate(mux))

	req := httptest.NewRequest("GET", "/api/v1/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/repositories status = %d, want 200 (scoped-token/browser-session 403 bug — rewrite must happen before auth)", rec.Code)
	}
	if rec.Body.String() != "/api/v0/repositories" {
		t.Fatalf("handler saw path = %q, want /api/v0/repositories", rec.Body.String())
	}
}

// TestV1Alias_WrongOrderFailsAuth validates that if the rewrite happened
// after the auth gate, scoped-token requests would 403. This test
// documents the layering requirement.
func TestV1Alias_WrongOrderFailsAuth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.Path))
	})

	authGate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/v0/") {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// Wrong order: auth wraps v1 rewrite — scoped-token sees /api/v1/ → 403
	handler := authGate(v1PrefixAliasMiddleware(mux))

	req := httptest.NewRequest("GET", "/api/v1/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /api/v1/repositories status = %d, want 403 (this test proves auth-before-rewrite fails)", rec.Code)
	}
}
