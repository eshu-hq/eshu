package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestNewAcceptedGenerationLookupUsesAcceptanceTable(t *testing.T) {
	t.Parallel()

	db := &acceptanceQueryCapturingDB{
		rows: []sharedProjectionAcceptanceRow{
			{
				scopeID:          "scope:git:repo-1",
				acceptanceUnitID: "repository:r_test",
				sourceRunID:      "run-test",
				generationID:     "gen-test",
				updatedAt:        time.Now().UTC(),
			},
		},
	}

	lookup := NewAcceptedGenerationLookup(db)
	gotGeneration, gotFound := lookup(reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope:git:repo-1",
		AcceptanceUnitID: "repository:r_test",
		SourceRunID:      "run-test",
	})

	if !gotFound {
		t.Fatal("lookup should find the acceptance row")
	}
	if gotGeneration != "gen-test" {
		t.Fatalf("generation = %q, want %q", gotGeneration, "gen-test")
	}

	if db.lastQuery == "" {
		t.Fatal("no query was executed")
	}
	if strings.Contains(db.lastQuery, "FROM fact_work_items") {
		t.Fatal("compatibility lookup must not query fact_work_items")
	}
	if !strings.Contains(db.lastQuery, "FROM shared_projection_acceptance") {
		t.Fatal("compatibility lookup should query shared_projection_acceptance")
	}
	if !strings.Contains(db.lastQuery, "WHERE scope_id = $1") {
		t.Fatal("lookup should filter by scope_id")
	}
	if !strings.Contains(db.lastQuery, "AND acceptance_unit_id = $2") {
		t.Fatal("lookup should filter by acceptance_unit_id")
	}
	if !strings.Contains(db.lastQuery, "AND source_run_id = $3") {
		t.Fatal("lookup should filter by source_run_id")
	}
	if len(db.lastArgs) != 3 {
		t.Fatalf("len(args) = %d, want 3", len(db.lastArgs))
	}
	if db.lastArgs[0] != "scope:git:repo-1" {
		t.Fatalf("arg[0] = %v, want scope:git:repo-1", db.lastArgs[0])
	}
	if db.lastArgs[1] != "repository:r_test" {
		t.Fatalf("arg[1] = %v, want repository:r_test", db.lastArgs[1])
	}
	if db.lastArgs[2] != "run-test" {
		t.Fatalf("arg[2] = %v, want run-test", db.lastArgs[2])
	}
}

func TestNewAcceptedGenerationLookupReturnsFalseOnStoreError(t *testing.T) {
	t.Parallel()

	lookup := NewAcceptedGenerationLookup(&acceptanceStoreErrorDB{})
	gotGeneration, gotFound := lookup(reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope:git:repo-1",
		AcceptanceUnitID: "repository:r_test",
		SourceRunID:      "run-test",
	})

	if gotGeneration != "" {
		t.Fatalf("generation = %q, want empty string", gotGeneration)
	}
	if gotFound {
		t.Fatal("found = true, want false when the store query fails")
	}
}

func TestNewAcceptedGenerationPrefetchCachesByAcceptanceKey(t *testing.T) {
	t.Parallel()

	db := &acceptanceQueryCapturingDB{
		rows: []sharedProjectionAcceptanceRow{
			{
				scopeID:          "scope:git:repo-1",
				acceptanceUnitID: "repository:r_test",
				sourceRunID:      "run-test",
				generationID:     "gen-test",
				updatedAt:        time.Now().UTC(),
			},
		},
	}

	prefetch := NewAcceptedGenerationPrefetch(db)
	lookup, err := prefetch(context.Background(), []reducer.SharedProjectionIntentRow{
		{
			ScopeID:          "scope:git:repo-1",
			AcceptanceUnitID: "repository:r_test",
			RepositoryID:     "repository:r_test",
			SourceRunID:      "run-test",
		},
		{
			ScopeID:          "scope:git:repo-1",
			AcceptanceUnitID: "repository:r_test",
			RepositoryID:     "repository:r_test",
			SourceRunID:      "run-test",
		},
	})
	if err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	gotGeneration, gotFound := lookup(reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope:git:repo-1",
		AcceptanceUnitID: "repository:r_test",
		SourceRunID:      "run-test",
	})
	if !gotFound {
		t.Fatal("lookup should find prefetched row")
	}
	if gotGeneration != "gen-test" {
		t.Fatalf("generation = %q, want gen-test", gotGeneration)
	}
}

type acceptanceQueryCapturingDB struct {
	rows      []sharedProjectionAcceptanceRow
	lastQuery string
	lastArgs  []any
}

func (db *acceptanceQueryCapturingDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext not implemented in test stub")
}

func (db *acceptanceQueryCapturingDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.lastQuery = query
	db.lastArgs = args
	return queryAcceptanceRows(db.rows, query, args...)
}

type acceptanceStoreErrorDB struct{}

func (db *acceptanceStoreErrorDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext not implemented in test stub")
}

func (db *acceptanceStoreErrorDB) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	return nil, fmt.Errorf("boom")
}
