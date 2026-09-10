// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// The two ContentHandler sections of this file moved with the handler family
// to internal/query/contentread (content_handler_index_readiness_test.go) in
// the #6060 lane-B1 move: they drive the handler's unexported search
// methods, which cannot be called from another package. The CodeHandler
// section below and the shared store stay in root.

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
	querytestutil.FakePortContentStore
}

func (contentSubstringIndexNotReadyStore) SearchFileContentAnyRepo(context.Context, string, int) ([]FileContent, error) {
	return nil, ErrContentSubstringIndexesNotReady
}

func (contentSubstringIndexNotReadyStore) SearchEntityContentAnyRepo(context.Context, string, int) ([]EntityContent, error) {
	return nil, ErrContentSubstringIndexesNotReady
}
