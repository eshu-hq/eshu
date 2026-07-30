// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonicalwriter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// fakeTerraformBackendQuery is a minimal tfstatebackend.TerraformBackendQuery
// stand-in so these tests can drive *tfstatebackend.Resolver without a real
// Postgres connection.
type fakeTerraformBackendQuery struct {
	rows []tfstatebackend.TerraformBackendRow
	err  error
}

func (f *fakeTerraformBackendQuery) ListTerraformBackendsByLocator(
	_ context.Context, _ string, _ string,
) ([]tfstatebackend.TerraformBackendRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]tfstatebackend.TerraformBackendRow, len(f.rows))
	copy(out, f.rows)
	return out, nil
}

// TestResolveOwningRepoIDOutcomeResolved proves the happy path: a single
// distinct owning repo maps to (repoID, TerraformStateOwnershipResolved).
func TestResolveOwningRepoIDOutcomeResolved(t *testing.T) {
	t.Parallel()

	rows := []tfstatebackend.TerraformBackendRow{{
		RepoID:           "repo-a",
		CommitID:         "aaa",
		CommitObservedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		BackendKind:      "s3",
		LocatorHash:      "hash-resolved",
	}}
	resolver := tfstatebackend.NewResolver(&fakeTerraformBackendQuery{rows: rows})

	repoID, outcome := ResolveOwningRepoIDOutcome(context.Background(), resolver, "s3", "hash-resolved")
	if got, want := repoID, "repo-a"; got != want {
		t.Fatalf("repoID = %q, want %q", got, want)
	}
	if got, want := outcome, projector.TerraformStateOwnershipResolved; got != want {
		t.Fatalf("outcome = %v, want %v", got, want)
	}
}

// TestResolveOwningRepoIDOutcomeNoOwner proves ErrNoConfigRepoOwnsBackend
// classifies as the AUTHORITATIVE TerraformStateOwnershipNoOwner outcome
// (#5623 P1 review, second finding) -- not the transient-failure bucket a
// prior version of this classification collapsed it into.
func TestResolveOwningRepoIDOutcomeNoOwner(t *testing.T) {
	t.Parallel()

	resolver := tfstatebackend.NewResolver(&fakeTerraformBackendQuery{}) // zero rows -> ErrNoConfigRepoOwnsBackend
	repoID, outcome := ResolveOwningRepoIDOutcome(context.Background(), resolver, "s3", "hash-unowned")
	if got, want := repoID, ""; got != want {
		t.Fatalf("repoID = %q, want empty", got)
	}
	if got, want := outcome, projector.TerraformStateOwnershipNoOwner; got != want {
		t.Fatalf("outcome = %v, want %v (NoOwner is authoritative, never TransientFailure)", got, want)
	}
}

// TestResolveOwningRepoIDOutcomeAmbiguousOwner proves ErrAmbiguousBackendOwner
// classifies as the AUTHORITATIVE TerraformStateOwnershipAmbiguousOwner
// outcome, the same review finding as NoOwner above.
func TestResolveOwningRepoIDOutcomeAmbiguousOwner(t *testing.T) {
	t.Parallel()

	rows := []tfstatebackend.TerraformBackendRow{
		{RepoID: "repo-a", CommitID: "aaa", CommitObservedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), BackendKind: "s3", LocatorHash: "hash-ambiguous"},
		{RepoID: "repo-b", CommitID: "bbb", CommitObservedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), BackendKind: "s3", LocatorHash: "hash-ambiguous"},
	}
	resolver := tfstatebackend.NewResolver(&fakeTerraformBackendQuery{rows: rows})
	repoID, outcome := ResolveOwningRepoIDOutcome(context.Background(), resolver, "s3", "hash-ambiguous")
	if got, want := repoID, ""; got != want {
		t.Fatalf("repoID = %q, want empty", got)
	}
	if got, want := outcome, projector.TerraformStateOwnershipAmbiguousOwner; got != want {
		t.Fatalf("outcome = %v, want %v (AmbiguousOwner is authoritative, never TransientFailure)", got, want)
	}
}

// TestResolveOwningRepoIDOutcomeTransientFailure proves an ordinary query
// error (a stand-in for a Postgres timeout or pool exhaustion) -- anything
// that is NOT one of the two documented sentinels -- classifies as
// TerraformStateOwnershipTransientFailure, distinct from the two
// authoritative outcomes above. This is the #5623 P1 review's own repro
// shape: a resolver hiccup must not be confused with an authoritative
// non-owner answer.
func TestResolveOwningRepoIDOutcomeTransientFailure(t *testing.T) {
	t.Parallel()

	resolver := tfstatebackend.NewResolver(&fakeTerraformBackendQuery{err: errors.New("fixture: postgres timeout")})
	repoID, outcome := ResolveOwningRepoIDOutcome(context.Background(), resolver, "s3", "hash-transient")
	if got, want := repoID, ""; got != want {
		t.Fatalf("repoID = %q, want empty", got)
	}
	if got, want := outcome, projector.TerraformStateOwnershipTransientFailure; got != want {
		t.Fatalf("outcome = %v, want %v", got, want)
	}
}
