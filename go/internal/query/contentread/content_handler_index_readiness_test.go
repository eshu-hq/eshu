// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// These two tests split from root's content_handler_index_readiness_test.go
// in the #6060 lane-B1 move: they drive the handler's unexported searchFiles
// and searchEntities, which cannot be called from another package. The
// CodeHandler section and the shared store stay in root; the store below is
// a local copy citing that original (a _test.go symbol is not importable
// across a package boundary).

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

type contentSubstringIndexNotReadyStore struct {
	querytestutil.FakePortContentStore
}

func (contentSubstringIndexNotReadyStore) SearchFileContentAnyRepo(context.Context, string, int) ([]querycontract.FileContent, error) {
	return nil, querycontract.ErrContentSubstringIndexesNotReady
}

func (contentSubstringIndexNotReadyStore) SearchEntityContentAnyRepo(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return nil, querycontract.ErrContentSubstringIndexesNotReady
}
