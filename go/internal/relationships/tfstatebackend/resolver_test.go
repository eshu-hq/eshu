// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstatebackend

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type stubQuery struct {
	rows []TerraformBackendRow
	err  error
}

func (s *stubQuery) ListTerraformBackendsByLocator(
	_ context.Context, _ string, _ string,
) ([]TerraformBackendRow, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]TerraformBackendRow, len(s.rows))
	copy(out, s.rows)
	return out, nil
}

func TestResolverReturnsErrNoConfigRepoOwnsBackend(t *testing.T) {
	t.Parallel()

	r := NewResolver(&stubQuery{})
	_, err := r.ResolveConfigCommitForBackend(context.Background(), "s3", "hash-1")
	if !errors.Is(err, ErrNoConfigRepoOwnsBackend) {
		t.Fatalf("err = %v, want ErrNoConfigRepoOwnsBackend", err)
	}
}

func TestResolverReturnsErrAmbiguousBackendOwner(t *testing.T) {
	t.Parallel()

	rows := []TerraformBackendRow{
		{
			RepoID:           "repo-a",
			ScopeID:          "repo:repo-a@1",
			CommitID:         "aaa",
			CommitObservedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
			BackendKind:      "s3",
			LocatorHash:      "hash-1",
		},
		{
			RepoID:           "repo-b",
			ScopeID:          "repo:repo-b@1",
			CommitID:         "bbb",
			CommitObservedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
			BackendKind:      "s3",
			LocatorHash:      "hash-1",
		},
	}
	r := NewResolver(&stubQuery{rows: rows})
	_, err := r.ResolveConfigCommitForBackend(context.Background(), "s3", "hash-1")
	if !errors.Is(err, ErrAmbiguousBackendOwner) {
		t.Fatalf("err = %v, want ErrAmbiguousBackendOwner", err)
	}
}

// TestResolverAmbiguousOwnerExposesCandidateRows proves the ambiguous-owner
// error carries every competing candidate row so a caller (the config-vs-state
// drift handler in particular) can record their identities as
// provenance-only evidence rather than dropping them once errors.Is confirms
// the ambiguous case. Before this, ErrAmbiguousBackendOwner was a bare
// sentinel with no way to recover which repos were competing.
func TestResolverAmbiguousOwnerExposesCandidateRows(t *testing.T) {
	t.Parallel()

	rows := []TerraformBackendRow{
		{
			RepoID:           "repo-a",
			ScopeID:          "repo:repo-a@1",
			CommitID:         "aaa",
			CommitObservedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
			BackendKind:      "s3",
			LocatorHash:      "hash-1",
		},
		{
			RepoID:           "repo-b",
			ScopeID:          "repo:repo-b@1",
			CommitID:         "bbb",
			CommitObservedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
			BackendKind:      "s3",
			LocatorHash:      "hash-1",
		},
	}
	r := NewResolver(&stubQuery{rows: rows})
	_, err := r.ResolveConfigCommitForBackend(context.Background(), "s3", "hash-1")

	var ambiguous *AmbiguousBackendOwnerError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("errors.As(err, *AmbiguousBackendOwnerError) = false, err = %v", err)
	}
	if got, want := len(ambiguous.Candidates), 2; got != want {
		t.Fatalf("len(Candidates) = %d, want %d", got, want)
	}
	// errors.Is must still see through to the bare sentinel: every existing
	// caller (aws_cloud_runtime_drift_evidence.go, incident_repository_
	// correlation_loader.go) matches on ErrAmbiguousBackendOwner directly.
	if !errors.Is(err, ErrAmbiguousBackendOwner) {
		t.Fatalf("errors.Is(err, ErrAmbiguousBackendOwner) = false, want true")
	}
}

func TestResolverSingleOwnerReturnsLatestByObservedAt(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	rows := []TerraformBackendRow{
		{
			RepoID:           "repo-a",
			ScopeID:          "repo:repo-a@1",
			CommitID:         "aaa",
			CommitObservedAt: older,
			BackendKind:      "s3",
			LocatorHash:      "hash-1",
		},
		{
			RepoID:           "repo-a",
			ScopeID:          "repo:repo-a@2",
			CommitID:         "bbb",
			CommitObservedAt: newer,
			BackendKind:      "s3",
			LocatorHash:      "hash-1",
		},
	}
	r := NewResolver(&stubQuery{rows: rows})
	anchor, err := r.ResolveConfigCommitForBackend(context.Background(), "s3", "hash-1")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if anchor.CommitID != "bbb" {
		t.Fatalf("CommitID = %q, want %q (latest by observed_at)", anchor.CommitID, "bbb")
	}
}

func TestResolverSingleOwnerTieBreaksByCommitIDAscending(t *testing.T) {
	t.Parallel()

	tied := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	// Provide rows out of order; the resolver must pick the lexicographically
	// SMALLEST commit_id when observed_at ties, per the design doc tie-break
	// (deterministic, no LWW).
	rows := []TerraformBackendRow{
		{
			RepoID:           "repo-a",
			ScopeID:          "repo:repo-a@z",
			CommitID:         "zzz",
			CommitObservedAt: tied,
			BackendKind:      "s3",
			LocatorHash:      "hash-1",
		},
		{
			RepoID:           "repo-a",
			ScopeID:          "repo:repo-a@a",
			CommitID:         "aaa",
			CommitObservedAt: tied,
			BackendKind:      "s3",
			LocatorHash:      "hash-1",
		},
	}
	r := NewResolver(&stubQuery{rows: rows})
	anchor, err := r.ResolveConfigCommitForBackend(context.Background(), "s3", "hash-1")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if anchor.CommitID != "aaa" {
		t.Fatalf("CommitID = %q, want %q (lex ascending tie-break)", anchor.CommitID, "aaa")
	}
}

// TestResolverSingleOwnerPreservesLocatorDefaulted proves
// ResolveConfigCommitForBackend's `return CommitAnchor(winner), nil` — a
// direct positional struct conversion, per the doc comment on
// TerraformBackendRow.LocatorDefaulted requiring the two structs to stay in
// identical field order — actually carries LocatorDefaulted through from the
// winning TerraformBackendRow into the returned CommitAnchor (issue #5594).
// No other test in the repo exercises this specific conversion path: the
// collector-level test (backend_config_local_test.go) only proves the
// candidate's own LocatorDefaulted is set correctly before it ever reaches
// the resolver, and the reducer-level test
// (terraform_config_state_drift_defaulted_locator_test.go) hand-builds a
// CommitAnchor directly rather than calling ResolveConfigCommitForBackend, so
// a field reordering between CommitAnchor and TerraformBackendRow that broke
// the positional conversion (silently mapping LocatorDefaulted onto the wrong
// field, or vice versa, since the conversion compiles as long as the field
// types line up positionally) would go undetected without this test.
func TestResolverSingleOwnerPreservesLocatorDefaulted(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	for _, defaulted := range []bool{true, false} {
		t.Run(fmt.Sprintf("defaulted=%v", defaulted), func(t *testing.T) {
			t.Parallel()

			rows := []TerraformBackendRow{
				{
					RepoID:           "repo-a",
					ScopeID:          "repo:repo-a@1",
					CommitID:         "aaa",
					CommitObservedAt: observedAt,
					BackendKind:      "local",
					LocatorHash:      "hash-1",
					LocatorDefaulted: defaulted,
				},
			}
			r := NewResolver(&stubQuery{rows: rows})
			anchor, err := r.ResolveConfigCommitForBackend(context.Background(), "local", "hash-1")
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if anchor.LocatorDefaulted != defaulted {
				t.Fatalf("anchor.LocatorDefaulted = %v, want %v", anchor.LocatorDefaulted, defaulted)
			}
		})
	}
}

func TestResolverRejectsBlankInputs(t *testing.T) {
	t.Parallel()

	r := NewResolver(&stubQuery{})
	if _, err := r.ResolveConfigCommitForBackend(context.Background(), "", "hash"); err == nil {
		t.Fatal("blank backend kind: err = nil, want non-nil")
	}
	if _, err := r.ResolveConfigCommitForBackend(context.Background(), "s3", ""); err == nil {
		t.Fatal("blank locator hash: err = nil, want non-nil")
	}
}

func TestResolverWithoutQueryReturnsErrNoConfigRepoOwnsBackend(t *testing.T) {
	t.Parallel()

	r := NewResolver(nil)
	_, err := r.ResolveConfigCommitForBackend(context.Background(), "s3", "hash-1")
	if !errors.Is(err, ErrNoConfigRepoOwnsBackend) {
		t.Fatalf("err = %v, want ErrNoConfigRepoOwnsBackend", err)
	}
}
