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

// TestChangedSinceRejectsRepositoryOutsideGrant is the #5167 F-6 proof for
// GET /api/v0/freshness/changed-since. The route takes ONE repository or scope
// selector straight from the query string, so the tenant question is not "which
// rows may this caller see" but "may this caller name this selector at all".
// Without a grant check a scoped token reads any repository in the deployment
// by asking for it.
func TestChangedSinceRejectsRepositoryOutsideGrant(t *testing.T) {
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
	// Granted repo-a only; asking for repo-b.
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("an ungranted selector must be indistinguishable from a missing one (404), matching queryselector.ResolveExactForAccess; got %d: %s", w.Code, w.Body.String())
	}
	if w.Code == http.StatusOK {
		t.Fatalf("a scoped caller granted only repo-a must not read changed-since for repo-b; got 200: %s", w.Body.String())
	}
	// The stronger half: the store must not be consulted for an ungranted
	// selector. Refusing after the read still exposes the row set to the
	// process and any logging or telemetry on that path.
	if reader.called {
		t.Fatalf("store was queried for an ungranted repository selector %q; the grant must be checked before the read", reader.filter.Repository)
	}
}
