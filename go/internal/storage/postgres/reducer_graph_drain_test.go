package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestReducerGraphDrainHasActiveReducerGraphWork(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{true}}}},
	}
	check := NewReducerGraphDrain(db)

	active, err := check.HasActiveReducerGraphWork(context.Background())
	if err != nil {
		t.Fatalf("HasActiveReducerGraphWork() error = %v, want nil", err)
	}
	if !active {
		t.Fatal("HasActiveReducerGraphWork() = false, want true")
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FROM fact_work_items",
		"stage = 'reducer'",
		"status IN ('pending', 'retrying', 'claimed', 'running')",
		"domain IN (",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}
