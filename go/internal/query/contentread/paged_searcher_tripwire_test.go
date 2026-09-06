// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// This file is the contentread half of the #6060 interface-export tripwire
// (the other 13 sections stay in root's interface_export_tripwire_test.go).
// It moved with the ContentHandler family in the lane-B1 move because it
// drives the handler's unexported searchFilesByScope/searchEntitiesByScope,
// which cannot be called from another package. The fake embeds
// querytestutil.FakePortContentStore -- the shared ContentStore double, not
// a redeclared one -- and adds only the two querycontract.PagedContentSearcher
// methods under test, so `ok` in the production type assertion is exactly as
// narrow as it would be for a real *ContentReader.

type fakePagedContentTripwireStore struct {
	querytestutil.FakePortContentStore
	fileCalls   int
	entityCalls int
}

func (s *fakePagedContentTripwireStore) SearchFiles(context.Context, string, []string, string, int, int) ([]querycontract.FileContent, error) {
	s.fileCalls++
	return nil, nil
}

func (s *fakePagedContentTripwireStore) SearchEntities(context.Context, string, []string, string, int, int) ([]querycontract.EntityContent, error) {
	s.entityCalls++
	return nil, nil
}

// TestContentHandlerSearchByScopeUsesPagedSearcherFastPath proves
// (h *ContentHandler).searchFilesByScope/searchEntitiesByScope's
// h.Content.(querycontract.PagedContentSearcher) assertion resolves to a real
// implementer rather than the per-repo SearchFileContent/SearchEntityContent
// loop fallback.
func TestContentHandlerSearchByScopeUsesPagedSearcherFastPath(t *testing.T) {
	t.Parallel()

	fake := &fakePagedContentTripwireStore{}
	h := &ContentHandler{Content: fake, Profile: querycontract.ProfileLocalAuthoritative}
	ctx := queryauth.ContextWithAuthContext(context.Background(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
	})
	req := contentSearchRequest{Pattern: "handle", RepoID: "repo-a", Limit: 10}

	if _, _, err := h.searchFilesByScope(ctx, req); err != nil {
		t.Fatalf("searchFilesByScope() error = %v, want nil", err)
	}
	if got, want := fake.fileCalls, 1; got != want {
		t.Fatalf("fileCalls = %d, want %d (fast path not taken)", got, want)
	}

	if _, _, err := h.searchEntitiesByScope(ctx, req); err != nil {
		t.Fatalf("searchEntitiesByScope() error = %v, want nil", err)
	}
	if got, want := fake.entityCalls, 1; got != want {
		t.Fatalf("entityCalls = %d, want %d (fast path not taken)", got, want)
	}
}
