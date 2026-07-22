// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

type fakeTerraformBackendQuery struct {
	rows []tfstatebackend.TerraformBackendRow
	err  error
}

func (f fakeTerraformBackendQuery) ListTerraformBackendsByLocator(
	_ context.Context, _ string, _ string,
) ([]tfstatebackend.TerraformBackendRow, error) {
	return f.rows, f.err
}

func TestProjectorTerraformStateOwnershipResolverSingleOwner(t *testing.T) {
	t.Parallel()

	query := fakeTerraformBackendQuery{rows: []tfstatebackend.TerraformBackendRow{{
		RepoID:           "repo-a",
		ScopeID:          "scope-a",
		CommitID:         "commit-a",
		CommitObservedAt: time.Now(),
		BackendKind:      "s3",
		LocatorHash:      "locator-a",
	}}}
	adapter := projectorTerraformStateOwnershipResolver{resolver: tfstatebackend.NewResolver(query)}

	repoID, ok := adapter.ResolveOwningRepoID(context.Background(), "s3", "locator-a")
	if !ok {
		t.Fatalf("ResolveOwningRepoID() ok = false, want true")
	}
	if got, want := repoID, "repo-a"; got != want {
		t.Fatalf("ResolveOwningRepoID() repoID = %q, want %q", got, want)
	}
}

func TestProjectorTerraformStateOwnershipResolverNoOwner(t *testing.T) {
	t.Parallel()

	adapter := projectorTerraformStateOwnershipResolver{resolver: tfstatebackend.NewResolver(fakeTerraformBackendQuery{})}

	repoID, ok := adapter.ResolveOwningRepoID(context.Background(), "s3", "locator-a")
	if ok {
		t.Fatalf("ResolveOwningRepoID() ok = true, want false for an unowned backend")
	}
	if repoID != "" {
		t.Fatalf("ResolveOwningRepoID() repoID = %q, want empty", repoID)
	}
}

func TestProjectorTerraformStateOwnershipResolverAmbiguousOwner(t *testing.T) {
	t.Parallel()

	query := fakeTerraformBackendQuery{rows: []tfstatebackend.TerraformBackendRow{
		{RepoID: "repo-a", BackendKind: "s3", LocatorHash: "locator-a", CommitObservedAt: time.Now()},
		{RepoID: "repo-b", BackendKind: "s3", LocatorHash: "locator-a", CommitObservedAt: time.Now()},
	}}
	adapter := projectorTerraformStateOwnershipResolver{resolver: tfstatebackend.NewResolver(query)}

	repoID, ok := adapter.ResolveOwningRepoID(context.Background(), "s3", "locator-a")
	if ok {
		t.Fatalf("ResolveOwningRepoID() ok = true, want false for an ambiguously-owned backend")
	}
	if repoID != "" {
		t.Fatalf("ResolveOwningRepoID() repoID = %q, want empty", repoID)
	}
}

func TestProjectorTerraformStateOwnershipResolverQueryFailure(t *testing.T) {
	t.Parallel()

	adapter := projectorTerraformStateOwnershipResolver{
		resolver: tfstatebackend.NewResolver(fakeTerraformBackendQuery{err: errors.New("boom")}),
	}

	repoID, ok := adapter.ResolveOwningRepoID(context.Background(), "s3", "locator-a")
	if ok {
		t.Fatalf("ResolveOwningRepoID() ok = true, want false when the query fails")
	}
	if repoID != "" {
		t.Fatalf("ResolveOwningRepoID() repoID = %q, want empty", repoID)
	}
}
