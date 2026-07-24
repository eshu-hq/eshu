// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIRepositoryListDocumentsBoundedGraphReadFailures(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/repositories")
	get := mustMapField(t, path, "get")
	responses := mustMapField(t, get, "responses")
	for _, status := range []string{"503", "504"} {
		if _, ok := responses[status]; !ok {
			t.Errorf("repository-list OpenAPI responses missing %s bounded graph-read response", status)
		}
	}
}

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

func TestListRepositoriesMapsGraphCountAvailabilityError(t *testing.T) {
	t.Parallel()
	const privateCause = "bolt://private.graph.invalid:7687"
	graph := fakeGraphReader{
		runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, fmt.Errorf("%s: %w", privateCause, ErrGraphUnavailable)
		},
	}
	handler := &RepositoryHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=1", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
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
