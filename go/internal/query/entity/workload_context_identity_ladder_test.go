// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	repositorycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// identityOnlyWorkloadContentStore names one workload for its repository
// through the read-model summary seam, without any materialized graph node.
type identityOnlyWorkloadContentStore struct {
	content.FakePortContentStore
	workloadNames []string
}

func (s identityOnlyWorkloadContentStore) RepositoryReadModelSummary(
	_ context.Context, _ string,
) (repositorycontract.RepositoryReadModelSummary, error) {
	return repositorycontract.RepositoryReadModelSummary{Available: true, WorkloadNames: s.workloadNames}, nil
}

var _ repositorycontract.RepositoryReadModelSummaryStore = identityOnlyWorkloadContentStore{}

// TestFetchServiceReadModelWorkloadContextEmitsScopedIDAndHandle is the T5
// identity row for the read-model fallback (compatibility doc section 1.4
// sites table): the response id is the scoped id synthesized from
// (repo_id, name) through workloadid.NewWorkloadID — the same constructor
// the graph write path uses, so the two agree — and a separate handle field
// carries the stable public workload:<name> string.
func TestFetchServiceReadModelWorkloadContextEmitsScopedIDAndHandle(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: identityOnlyWorkloadContentStore{
			FakePortContentStore: content.FakePortContentStore{
				Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repository:r_a", Name: "alpha"}},
			},
			workloadNames: []string{"checkout"},
		},
	}
	ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repository:r_a"},
	})

	result, err := handler.FetchServiceReadModelWorkloadContext(ctx, "checkout")
	if err != nil {
		t.Fatalf("FetchServiceReadModelWorkloadContext() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("result = nil, want the identity-only fallback row")
	}
	wantID := workloadid.NewWorkloadID("repository:r_a", "checkout").String()
	if got := querycontract.StringVal(result, "id"); got != wantID {
		t.Fatalf("id = %q, want scoped id %q", got, wantID)
	}
	if got, want := querycontract.StringVal(result, "handle"), "workload:checkout"; got != want {
		t.Fatalf("handle = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(result, "name"), "checkout"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(result, "repo_id"), "repository:r_a"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
}
