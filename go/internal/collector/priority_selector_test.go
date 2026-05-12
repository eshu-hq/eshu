package collector

import (
	"context"
	"testing"
	"time"
)

func TestPriorityRepositorySelectorUsesFirstNonEmptyBatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 16, 0, 0, 0, time.UTC)
	selector := PriorityRepositorySelector{
		Selectors: []RepositorySelector{
			priorityStubRepositorySelector{batch: SelectionBatch{ObservedAt: now}},
			priorityStubRepositorySelector{batch: SelectionBatch{
				ObservedAt:   now,
				Repositories: []SelectedRepository{{RepoPath: "/tmp/repo-a"}},
			}},
			priorityStubRepositorySelector{err: errShouldNotCallSelector{}},
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(batch.Repositories) = %d, want %d", got, want)
	}
}

type priorityStubRepositorySelector struct {
	batch SelectionBatch
	err   error
}

func (s priorityStubRepositorySelector) SelectRepositories(context.Context) (SelectionBatch, error) {
	return s.batch, s.err
}

type errShouldNotCallSelector struct{}

func (errShouldNotCallSelector) Error() string { return "selector should not be called" }
