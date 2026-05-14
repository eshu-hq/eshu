package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFactStoreListActiveRepositoryFactsUsesActiveGenerations(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-repo-1",
					"repository:repo-1",
					"generation-1",
					"repository",
					"repository:repo-1",
					"1.0.0",
					"git",
					int64(0),
					"unknown",
					"git",
					"repository:repo-1",
					"file:///repo/path",
					"repo-1",
					time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"repo_id":"repo-1","graph_id":"repo-1","remote_url":"git@github.com:acme/team-api.git"}`),
				}},
			},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListActiveRepositoryFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveRepositoryFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("ListActiveRepositoryFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "repository"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = 'repository'",
		"fact.source_system = 'git'",
		"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
		"LIMIT $3",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}
