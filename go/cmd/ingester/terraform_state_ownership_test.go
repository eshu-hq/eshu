// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// These tests prove ingesterTerraformStateOwnershipResolver.ResolveOwningRepoID
// correctly delegates to canonicalwriter.ResolveOwningRepoIDOutcome (#5623 P1
// review, second finding) against the REAL *tfstatebackend.Resolver, not just
// that the shared helper itself works (that is
// internal/relationships/tfstatebackend/canonicalwriter's own job) -- this is
// the end-to-end wiring proof for this specific concrete adapter type.
// cmd/bootstrap-index and cmd/projector carry the identical four tests
// against their own adapter types.

type fakeTerraformBackendQuery struct {
	rows []tfstatebackend.TerraformBackendRow
	err  error
}

func (f fakeTerraformBackendQuery) ListTerraformBackendsByLocator(
	_ context.Context, _ string, _ string,
) ([]tfstatebackend.TerraformBackendRow, error) {
	return f.rows, f.err
}

func TestIngesterTerraformStateOwnershipResolverSingleOwner(t *testing.T) {
	t.Parallel()

	query := fakeTerraformBackendQuery{rows: []tfstatebackend.TerraformBackendRow{{
		RepoID:           "repo-a",
		ScopeID:          "scope-a",
		CommitID:         "commit-a",
		CommitObservedAt: time.Now(),
		BackendKind:      "s3",
		LocatorHash:      "locator-a",
	}}}
	adapter := ingesterTerraformStateOwnershipResolver{resolver: tfstatebackend.NewResolver(query)}

	repoID, outcome := adapter.ResolveOwningRepoID(context.Background(), "s3", "locator-a")
	if got, want := outcome, projector.TerraformStateOwnershipResolved; got != want {
		t.Fatalf("ResolveOwningRepoID() outcome = %v, want %v", got, want)
	}
	if got, want := repoID, "repo-a"; got != want {
		t.Fatalf("ResolveOwningRepoID() repoID = %q, want %q", got, want)
	}
}

// TestIngesterTerraformStateOwnershipResolverNoOwner proves the #5623 P1
// review's own repro shape: an unowned backend must classify as the
// AUTHORITATIVE TerraformStateOwnershipNoOwner outcome, never
// TerraformStateOwnershipTransientFailure. cmd/ingester is the binary that
// actually runs the deployed StatefulSet (per cmd/ingester/README.md), so
// this is the production-critical instance of this proof.
func TestIngesterTerraformStateOwnershipResolverNoOwner(t *testing.T) {
	t.Parallel()

	adapter := ingesterTerraformStateOwnershipResolver{resolver: tfstatebackend.NewResolver(fakeTerraformBackendQuery{})}

	repoID, outcome := adapter.ResolveOwningRepoID(context.Background(), "s3", "locator-a")
	if got, want := outcome, projector.TerraformStateOwnershipNoOwner; got != want {
		t.Fatalf("ResolveOwningRepoID() outcome = %v, want %v for an unowned backend", got, want)
	}
	if repoID != "" {
		t.Fatalf("ResolveOwningRepoID() repoID = %q, want empty", repoID)
	}
}

// TestIngesterTerraformStateOwnershipResolverAmbiguousOwner is the
// AmbiguousOwner counterpart to NoOwner above -- the same review finding.
func TestIngesterTerraformStateOwnershipResolverAmbiguousOwner(t *testing.T) {
	t.Parallel()

	query := fakeTerraformBackendQuery{rows: []tfstatebackend.TerraformBackendRow{
		{RepoID: "repo-a", BackendKind: "s3", LocatorHash: "locator-a", CommitObservedAt: time.Now()},
		{RepoID: "repo-b", BackendKind: "s3", LocatorHash: "locator-a", CommitObservedAt: time.Now()},
	}}
	adapter := ingesterTerraformStateOwnershipResolver{resolver: tfstatebackend.NewResolver(query)}

	repoID, outcome := adapter.ResolveOwningRepoID(context.Background(), "s3", "locator-a")
	if got, want := outcome, projector.TerraformStateOwnershipAmbiguousOwner; got != want {
		t.Fatalf("ResolveOwningRepoID() outcome = %v, want %v for an ambiguously-owned backend", got, want)
	}
	if repoID != "" {
		t.Fatalf("ResolveOwningRepoID() repoID = %q, want empty", repoID)
	}
}

// TestIngesterTerraformStateOwnershipResolverQueryFailure proves an ordinary
// query error (never one of the two documented sentinels) classifies as
// TerraformStateOwnershipTransientFailure -- distinct from NoOwner/AmbiguousOwner
// above.
func TestIngesterTerraformStateOwnershipResolverQueryFailure(t *testing.T) {
	t.Parallel()

	adapter := ingesterTerraformStateOwnershipResolver{
		resolver: tfstatebackend.NewResolver(fakeTerraformBackendQuery{err: errors.New("boom")}),
	}

	repoID, outcome := adapter.ResolveOwningRepoID(context.Background(), "s3", "locator-a")
	if got, want := outcome, projector.TerraformStateOwnershipTransientFailure; got != want {
		t.Fatalf("ResolveOwningRepoID() outcome = %v, want %v when the query fails", got, want)
	}
	if repoID != "" {
		t.Fatalf("ResolveOwningRepoID() repoID = %q, want empty", repoID)
	}
}
