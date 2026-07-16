// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRepositoriesDoesNotPublishZeroWhenGraphCountFails(t *testing.T) {
	t.Parallel()

	pageCalls := 0
	graph := fakeGraphReader{
		runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, errors.New("count unavailable")
		},
		run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			pageCalls++
			return []map[string]any{{"id": "repository:one", "name": "one"}}, nil
		},
	}
	handler := &RepositoryHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=1", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got := pageCalls; got != 0 {
		t.Fatalf("page query calls = %d, want 0 after authoritative count failure", got)
	}
	if got := rec.Body.String(); got == "0" {
		t.Fatalf("body = %q, must not publish failed count as exact zero", got)
	}
}
