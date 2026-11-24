// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeprecationHeaders_PresentOnV0Routes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	const sunset = "Thu, 01 Jul 2027 00:00:00 GMT"
	deprecated := deprecationHeadersMiddleware(mux, sunset)

	req := httptest.NewRequest("GET", "/api/v0/test", nil)
	rec := httptest.NewRecorder()
	deprecated.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v0/test status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("Deprecation header = %q, want 'true'", got)
	}
	if got := rec.Header().Get("Sunset"); got != sunset {
		t.Fatalf("Sunset header = %q, want %q", got, sunset)
	}
}

func TestDeprecationHeaders_AbsentOnV1Routes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	const sunset = "Thu, 01 Jul 2027 00:00:00 GMT"
	// Full production layering: deprecation OUTER, v1 alias INNER.
	handler := deprecationHeadersMiddleware(v1PrefixAliasMiddleware(mux), sunset)

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/test status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Deprecation"); got != "" {
		t.Fatalf("Deprecation header = %q, want empty on v1", got)
	}
	if got := rec.Header().Get("Sunset"); got != "" {
		t.Fatalf("Sunset header = %q, want empty on v1", got)
	}
}

func TestDeprecationHeaders_AbsentOnNonAPIRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	const sunset = "Thu, 01 Jul 2027 00:00:00 GMT"
	deprecated := deprecationHeadersMiddleware(mux, sunset)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	deprecated.ServeHTTP(rec, req)

	if got := rec.Header().Get("Deprecation"); got != "" {
		t.Fatalf("Deprecation header = %q, want empty on non-API route", got)
	}
	if got := rec.Header().Get("Sunset"); got != "" {
		t.Fatalf("Sunset header = %q, want empty on non-API route", got)
	}
}

// TestDeprecationHeaders_V1AfterRewrite tests the full layering:
// deprecation middleware → v1PrefixAliasMiddleware → mux.
// V1 requests must not carry deprecation headers even after the
// path is rewritten to /api/v0/ inside the v1 alias middleware.
func TestDeprecationHeaders_V1AfterRewrite(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})

	const sunset = "Thu, 01 Jul 2027 00:00:00 GMT"
	// Correct production order: deprecation OUTER, v1 rewrite INNER.
	// deprecation sees original /api/v1/* path → no headers → v1 rewrites → ok.
	// deprecation sees original /api/v0/* path → adds headers → v1 no-op → ok.
	handler := deprecationHeadersMiddleware(v1PrefixAliasMiddleware(mux), sunset)

	t.Run("v1 request has no deprecation headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/repositories", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Deprecation"); got != "" {
			t.Fatalf("Deprecation header on v1 = %q, want empty", got)
		}
		if got := rec.Header().Get("Sunset"); got != "" {
			t.Fatalf("Sunset header on v1 = %q, want empty", got)
		}
	})

	t.Run("v0 request has deprecation headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v0/repositories", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Deprecation"); got != "true" {
			t.Fatalf("Deprecation header on v0 = %q, want 'true'", got)
		}
		if got := rec.Header().Get("Sunset"); got != sunset {
			t.Fatalf("Sunset header on v0 = %q, want %q", got, sunset)
		}
	})
}

// TestDeprecationHeaders_WrongOrder proves the layering requirement.
// If v1 rewrite wraps deprecation, v1 requests incorrectly carry headers.
func TestDeprecationHeaders_WrongOrder(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	const sunset = "Thu, 01 Jul 2027 00:00:00 GMT"
	// Wrong order: v1 rewrite OUTER, deprecation INNER.
	// v1 path gets rewritten to /api/v0/ before deprecation sees it → incorrectly adds headers.
	handler := v1PrefixAliasMiddleware(deprecationHeadersMiddleware(mux, sunset))

	req := httptest.NewRequest("GET", "/api/v1/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// With wrong order, v1 path gets rewritten to /api/v0/ before deprecation
	// middleware sees it, so headers incorrectly appear on v1 responses.
	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("Deprecation header = %q, want 'true' (wrong order: v1 rewrite happens before deprecation check, rewriting /api/v1/→/api/v0/ before the middleware sees it)", got)
	}
	// This proves the wrong order is buggy and the correct order is required.
}

func TestDeprecationHeaders_SunsetDateFromEnv(t *testing.T) {
	custom := "Fri, 31 Dec 2027 23:59:59 GMT"

	// Simulate reading the sunset date from an environment variable
	// by calling the same default logic used in wireAPI.
	getenv := func(key string) string {
		if key == "ESHU_API_V0_SUNSET_DATE" {
			return custom
		}
		return ""
	}

	sunsetDate := getenv("ESHU_API_V0_SUNSET_DATE")
	if sunsetDate == "" {
		sunsetDate = "Thu, 01 Jul 2027 00:00:00 GMT"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	deprecated := deprecationHeadersMiddleware(mux, sunsetDate)

	req := httptest.NewRequest("GET", "/api/v0/test", nil)
	rec := httptest.NewRecorder()
	deprecated.ServeHTTP(rec, req)

	if got := rec.Header().Get("Sunset"); got != custom {
		t.Fatalf("Sunset header = %q, want %q", got, custom)
	}
}

func TestDeprecationHeaders_PresentOnV0ErrorResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/notfound", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	const sunset = "Thu, 01 Jul 2027 00:00:00 GMT"
	deprecated := deprecationHeadersMiddleware(mux, sunset)

	req := httptest.NewRequest("GET", "/api/v0/notfound", nil)
	rec := httptest.NewRecorder()
	deprecated.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	// Headers are set before the handler runs, so they appear on errors too.
	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("Deprecation header on 404 = %q, want 'true'", got)
	}
}

func TestDeprecationHeaders_DeepV0Path(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/story", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	const sunset = "Thu, 01 Jul 2027 00:00:00 GMT"
	deprecated := deprecationHeadersMiddleware(mux, sunset)

	req := httptest.NewRequest("GET", "/api/v0/repositories/repo-123/story", nil)
	rec := httptest.NewRecorder()
	deprecated.ServeHTTP(rec, req)

	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("Deprecation header = %q, want 'true'", got)
	}
}

func TestDeprecationHeaders_MultipleV0Calls(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/a", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/v0/b", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	const sunset = "Thu, 01 Jul 2027 00:00:00 GMT"
	deprecated := deprecationHeadersMiddleware(mux, sunset)

	for _, tc := range []struct {
		method, path string
		wantCode     int
	}{
		{"GET", "/api/v0/a", 200},
		{"POST", "/api/v0/b", 201},
		{"GET", "/api/v0/a", 200},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			deprecated.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if got := rec.Header().Get("Deprecation"); got != "true" {
				t.Fatalf("Deprecation header = %q, want 'true'", got)
			}
		})
	}
}
