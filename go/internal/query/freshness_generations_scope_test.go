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

// TestGenerationLifecycleBindsGrantForGenerationIDOnlyQuery is the #5167
// regression for the bypass review found in the first attempt at this fix. A
// guard that inspected only the repository and scope selector fields let a
// scoped caller leave both blank, pass another tenant's generation_id, and
// receive that generation's scope, queue counts and latest failure message.
//
// The grant now travels into the filter, so it binds the row regardless of
// which other fields the caller supplied.
func TestGenerationLifecycleBindsGrantForGenerationIDOnlyQuery(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerations{}
	handler := &FreshnessHandler{Generations: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/generations?generation_id=someone-elses-generation",
		nil,
	)
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

	if !reader.filter.Scoped {
		t.Fatalf("a scoped caller must reach the store with Scoped set, so the query can bind the grant; filter = %+v", reader.filter)
	}
	if len(reader.filter.AllowedRepositoryIDs) == 0 && len(reader.filter.AllowedScopeIDs) == 0 {
		t.Fatalf("the grant must travel into the filter for a generation_id-only query, or the query cannot bound the row; filter = %+v", reader.filter)
	}
}

// TestGenerationLifecycleLeavesSharedKeyUnbounded proves the binding is not
// applied to a shared-key caller, which would silently narrow an operator's
// deployment-wide view.
func TestGenerationLifecycleLeavesSharedKeyUnbounded(t *testing.T) {
	t.Parallel()

	reader := &recordingGenerations{}
	handler := &FreshnessHandler{Generations: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/freshness/generations?repository=repo-b", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeShared}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if reader.filter.Scoped {
		t.Fatalf("a shared-key caller must not be bound by a scoped grant; filter = %+v", reader.filter)
	}
}
