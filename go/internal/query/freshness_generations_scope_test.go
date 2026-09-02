// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

type recordingGenerations struct {
	called bool
	filter status.GenerationLifecycleFilter
}

func (r *recordingGenerations) ListGenerationLifecycle(
	_ context.Context, filter status.GenerationLifecycleFilter,
) (status.GenerationLifecyclePage, error) {
	r.called = true
	r.filter = filter
	return status.GenerationLifecyclePage{}, nil
}

// TestGenerationLifecycleRejectsRepositoryOutsideGrant is the #5167 F-6 proof
// for GET /api/v0/freshness/generations. Like changed-since it names one
// repository or scope selector taken from the query string, so a scoped caller
// without this check reads any repository's generation lifecycle by asking.
func TestGenerationLifecycleRejectsRepositoryOutsideGrant(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerations{}
	handler := &FreshnessHandler{Generations: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/freshness/generations?repository=repo-b", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("a scoped caller granted only repo-a must not list generations for repo-b; got 200: %s", w.Body.String())
	}
	if reader.called {
		t.Fatalf("store was queried for an ungranted repository selector %q; the grant must be checked before the read", reader.filter.Repository)
	}
}

// TestGenerationLifecycleAllowsGrantedRepository is the other half: the guard
// must not turn a legitimate scoped caller away, which a status-code-only
// assertion on the negative case would never catch.
func TestGenerationLifecycleAllowsGrantedRepository(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerations{}
	handler := &FreshnessHandler{Generations: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/freshness/generations?repository=repo-a", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if !reader.called {
		t.Fatalf("a granted repository must still reach the store; got status %d body %s", w.Code, w.Body.String())
	}
}
