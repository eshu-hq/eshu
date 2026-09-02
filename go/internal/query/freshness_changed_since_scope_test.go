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

// recordingChangedSince captures whether the store was reached at all, which is
// the half a status-code assertion cannot see: a handler that returns 403 only
// after querying has already read another tenant's rows.
type recordingChangedSince struct {
	called bool
	filter status.ChangedSinceFilter
}

func (r *recordingChangedSince) ComputeChangedSinceDelta(
	_ context.Context, filter status.ChangedSinceFilter,
) (status.ChangedSinceSummary, error) {
	r.called = true
	r.filter = filter
	return status.ChangedSinceSummary{}, nil
}

// TestChangedSinceBindsGrantIntoFilter proves the grant reaches the store,
// where it binds the scope row. It is asserted at the filter rather than by
// status code on purpose: an ungranted scope resolves to no row and returns the
// route's ordinary scope-not-found contract error, which is byte-identical to
// what a caller sees for a scope that does not exist. A status assertion
// therefore cannot tell "bound correctly" from "not bound at all".
func TestChangedSinceBindsGrantIntoFilter(t *testing.T) {
	t.Parallel()

	reader := &recordingChangedSince{}
	handler := &FreshnessHandler{ChangedSince: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/changed-since?repository=repo-b&since_generation_id=gen-prior",
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
		t.Fatalf("a scoped caller must reach the store with Scoped set so the query can bind the grant; filter = %+v", reader.filter)
	}
	if len(reader.filter.AllowedRepositoryIDs) == 0 && len(reader.filter.AllowedScopeIDs) == 0 {
		t.Fatalf("the grant must travel into the filter; filter = %+v", reader.filter)
	}
}

// TestChangedSinceLeavesSharedKeyUnbounded guards the other direction: an
// operator using the shared key must not have their view silently narrowed.
func TestChangedSinceLeavesSharedKeyUnbounded(t *testing.T) {
	t.Parallel()

	reader := &recordingChangedSince{}
	handler := &FreshnessHandler{ChangedSince: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/changed-since?repository=repo-b&since_generation_id=gen-prior",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeShared}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if !reader.called {
		t.Fatalf("the shared-key case must exercise the production path; the store was never called, so a false Scoped would only be the zero value (status %d, body %s)", w.Code, w.Body.String())
	}
	if reader.filter.Scoped {
		t.Fatalf("a shared-key caller must not be bound by a scoped grant; filter = %+v", reader.filter)
	}
}
