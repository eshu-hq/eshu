// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentHandlerSearchFilesReturns503UntilSubstringIndexesReady(t *testing.T) {
	t.Parallel()

	handler := &ContentHandler{Content: contentSubstringIndexNotReadyStore{}}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"renderApp","limit":10}`),
	)
	rec := httptest.NewRecorder()

	handler.searchFiles(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestContentHandlerSearchEntitiesReturns503UntilSubstringIndexesReady(t *testing.T) {
	t.Parallel()

	handler := &ContentHandler{Content: contentSubstringIndexNotReadyStore{}}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"pattern":"renderApp","limit":10}`),
	)
	rec := httptest.NewRecorder()

	handler.searchEntities(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestCodeHandlerSearchReturns503UntilSubstringIndexesReady(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: contentSubstringIndexNotReadyStore{}}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"renderApp","limit":10}`),
	)
	rec := httptest.NewRecorder()

	handler.handleSearch(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

type contentSubstringIndexNotReadyStore struct {
	fakePortContentStore
}

func (contentSubstringIndexNotReadyStore) SearchFileContentAnyRepo(context.Context, string, int) ([]FileContent, error) {
	return nil, ErrContentSubstringIndexesNotReady
}

func (contentSubstringIndexNotReadyStore) SearchEntityContentAnyRepo(context.Context, string, int) ([]EntityContent, error) {
	return nil, ErrContentSubstringIndexesNotReady
}
