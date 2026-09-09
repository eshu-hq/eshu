// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestCodeHandlerFindSymbolRejectsHugeOffset(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: querytestutil.FakePortContentStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/symbols/search",
		bytes.NewBufferString(`{"symbol":"renderApp","offset":10001}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "offset must be <= 10000") {
		t.Fatalf("body = %s, want offset bound error", w.Body.String())
	}
}

func TestCodeHandlerFindSymbolRejectsGraphOnlyOffset(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: &stubGraphReader{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/symbols/search",
		bytes.NewBufferString(`{"symbol":"renderApp","offset":1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "requires content-index search") {
		t.Fatalf("body = %s, want content-index offset error", w.Body.String())
	}
}

func TestCodeHandlerFindSymbolRejectsMissingBackends(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/symbols/search",
		bytes.NewBufferString(`{"symbol":"renderApp"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "symbol lookup backend is unavailable") {
		t.Fatalf("body = %s, want backend unavailable error", w.Body.String())
	}
}
