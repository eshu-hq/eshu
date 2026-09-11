// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestListRepositoriesDoesNotPublishZeroWhenGraphCountFails(t *testing.T) {
	t.Parallel()

	pageCalls := 0
	graph := querytestutil.FakeGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, errors.New("count unavailable")
		},
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			pageCalls++
			return []map[string]any{{"id": "repository:one", "name": "one"}}, nil
		},
	}
	handler := &Handler{Neo4j: graph, Profile: querycontract.ProfileLocalAuthoritative}
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

func TestListRepositoriesMapsGraphCountAvailabilityError(t *testing.T) {
	t.Parallel()
	const privateCause = "bolt://private.graph.invalid:7687"
	graph := querytestutil.FakeGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, fmt.Errorf("%s: %w", privateCause, querycontract.ErrGraphUnavailable)
		},
	}
	handler := &Handler{Neo4j: graph, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=1", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"backend_unavailable"`) {
		t.Fatalf("body = %s, want backend_unavailable", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), privateCause) {
		t.Fatalf("body leaked private graph cause: %s", rec.Body.String())
	}
}
