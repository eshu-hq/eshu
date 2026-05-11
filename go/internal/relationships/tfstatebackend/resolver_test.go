package tfstatebackend

import (
	"context"
	"errors"
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
