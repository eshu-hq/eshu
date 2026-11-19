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
