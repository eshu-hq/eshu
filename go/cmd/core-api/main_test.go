// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type v1CaptureHandler struct {
	t        *testing.T
	wantPath string
}

func (h v1CaptureHandler) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	h.t.Helper()
	if r.URL.Path != h.wantPath {
		h.t.Fatalf("rewritten path = %q, want %q", r.URL.Path, h.wantPath)
	}
}

func v1Request(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, path, nil)
}

type v1HeaderRecorder struct {
	header http.Header
	code   int
}

func (w *v1HeaderRecorder) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *v1HeaderRecorder) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *v1HeaderRecorder) WriteHeader(code int) {
	w.code = code
}

type v1OKHandler struct{}

func (v1OKHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func errTestMissingKey() error {
	return errors.New("provider secret encryption key is not configured")
}

func errTestOther() error {
	return errors.New("boom")
}

func TestRunVersionFlagPrintsAndExitsZero(t *testing.T) {
	t.Parallel()
	if got := run([]string{"--version"}, func(string) string { return "" }, os.Stderr); got != 0 {
		t.Fatalf("run --version = %d, want 0", got)
	}
}

func TestCoreShutdownTimeoutDefaultsToThirtySeconds(t *testing.T) {
	t.Parallel()
	if got := coreShutdownTimeout(nil); got.String() != "30s" {
		t.Fatalf("default shutdown timeout = %s, want 30s", got)
	}
	if got := coreShutdownTimeout(func(string) string { return "bogus" }); got.String() != "30s" {
		t.Fatalf("invalid shutdown timeout = %s, want 30s fallback", got)
	}
	if got := coreShutdownTimeout(func(string) string { return "5s" }); got.String() != "5s" {
		t.Fatalf("shutdown timeout = %s, want 5s", got)
	}
}

func TestReadCoreSunsetDateDefaultsPinned(t *testing.T) {
	t.Parallel()
	const want = "Thu, 01 Jul 2027 00:00:00 GMT"
	if got := readCoreSunsetDate(nil); got != want {
		t.Fatalf("default sunset = %q, want %q", got, want)
	}
	if got := readCoreSunsetDate(func(string) string { return "bogus" }); got != want {
		t.Fatalf("invalid sunset = %q, want %q", got, want)
	}
}

func TestV1PrefixAliasMiddlewareRewrites(t *testing.T) {
	t.Parallel()
	next := v1PrefixAliasMiddleware(v1CaptureHandler{t: t, wantPath: "/api/v0/repositories"})
	next.ServeHTTP(nil, v1Request("/api/v1/repositories"))
}

func TestCoreOuterMiddlewareV1CarriesNoDeprecation(t *testing.T) {
	t.Parallel()
	// The production outer chain must leave /api/v1/ responses free of
	// Deprecation/Sunset (v1 is the current spelling, not the deprecated
	// one) while /api/v0/ responses keep both headers. wrapCoreAPIMiddleware
	// is the exact chain serve() installs, so this pins the nesting order,
	// not the individual middlewares.
	v1Rec := &v1HeaderRecorder{}
	wrapCoreAPIMiddleware(v1CaptureHandler{t: t, wantPath: "/api/v0/repositories"}, "test-sunset").
		ServeHTTP(v1Rec, v1Request("/api/v1/repositories"))
	if got := v1Rec.Header().Get("Sunset"); got != "" {
		t.Fatalf("/api/v1/ Sunset = %q, want empty (rewrite runs inside the header middleware)", got)
	}
	if got := v1Rec.Header().Get("Deprecation"); got != "" {
		t.Fatalf("/api/v1/ Deprecation = %q, want empty", got)
	}

	v0Rec := &v1HeaderRecorder{}
	wrapCoreAPIMiddleware(v1OKHandler{}, "test-sunset").
		ServeHTTP(v0Rec, v1Request("/api/v0/repositories"))
	if got := v0Rec.Header().Get("Sunset"); got != "test-sunset" {
		t.Fatalf("/api/v0/ Sunset = %q, want test-sunset", got)
	}
	if got := v0Rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("/api/v0/ Deprecation = %q, want true", got)
	}
}

func TestDeprecationHeadersMiddlewareSetsSunset(t *testing.T) {
	t.Parallel()
	rec := &v1HeaderRecorder{}
	deprecationHeadersMiddleware(v1OKHandler{}, "test-sunset").ServeHTTP(rec, v1Request("/api/v0/repositories"))
	if rec.Header().Get("Sunset") != "test-sunset" || rec.Header().Get("Deprecation") != "true" {
		t.Fatalf("deprecation headers = %v, want Sunset+Deprecation", rec.Header())
	}
	rec2 := &v1HeaderRecorder{}
	deprecationHeadersMiddleware(v1OKHandler{}, "test-sunset").ServeHTTP(rec2, v1Request("/other"))
	if rec2.Header().Get("Sunset") != "" {
		t.Fatalf("non-/api/v0/ path must not carry Sunset, got %v", rec2.Header())
	}
}

func TestCoreDeferredHandlerStartsWith503(t *testing.T) {
	t.Parallel()
	h := &coreDeferredHandler{}
	rec := &v1HeaderRecorder{code: 200}
	h.ServeHTTP(rec, v1Request("/api/v0/repositories"))
	if rec.code != 503 {
		t.Fatalf("unset deferred handler = %d, want 503", rec.code)
	}
}

func TestIsKeyNotConfiguredMatchesMissingKey(t *testing.T) {
	t.Parallel()
	if !isKeyNotConfigured(errTestMissingKey()) {
		t.Fatal("expected missing-key error to match")
	}
	if isKeyNotConfigured(nil) {
		t.Fatal("nil error must not match")
	}
	if isKeyNotConfigured(errTestOther()) {
		t.Fatal("unrelated error must not match")
	}
}

func TestCoreEnvOrEmptyReadsTrimmed(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		if key == "PRESENT" {
			return "  value  "
		}
		return ""
	}
	if got := coreEnvOrEmpty(getenv, "PRESENT"); got != "value" {
		t.Fatalf("coreEnvOrEmpty = %q, want trimmed value", got)
	}
	if got := coreEnvOrEmpty(getenv, "ABSENT"); got != "" {
		t.Fatalf("coreEnvOrEmpty absent = %q, want empty", got)
	}
	if got := coreEnvOrEmpty(nil, "ABSENT"); got != "" {
		t.Fatalf("coreEnvOrEmpty nil getenv = %q, want empty", got)
	}
}

func TestSplitCoreEnvListSkipsBlanks(t *testing.T) {
	t.Parallel()
	got := splitCoreEnvList("a,, b ,")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("splitCoreEnvList = %q, want [a b]", strings.Join(got, ","))
	}
}

// TestCoreMetricsMountIsIntentionallyUnfrozen pins /metrics as a deliberate
// carve-out: an infrastructure scrape endpoint mounted by the binary, never
// forwarded by the satellite, hence absent from the frozen served-method
// list by design rather than by drift. A nil handler mounts nothing.
func TestCoreMetricsMountIsIntentionallyUnfrozen(t *testing.T) {
	t.Parallel()
	served := false
	mux := http.NewServeMux()
	mountCoreMetrics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true }))(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK || !served {
		t.Fatalf("/metrics = %d served=%v, want 200 served=true", rec.Code, served)
	}
	bare := http.NewServeMux()
	mountCoreMetrics(nil)(bare, nil)
	rec2 := httptest.NewRecorder()
	bare.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("/metrics with nil handler = %d, want 404 (unmounted)", rec2.Code)
	}
}
