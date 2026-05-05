package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSharedProjectionAcceptanceStoreUpsertAndLookup(t *testing.T) {
	t.Parallel()

	db := newSharedProjectionAcceptanceTestDB()
	store := NewSharedProjectionAcceptanceStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	rows := []SharedProjectionAcceptance{
		{
			ScopeID:          "scope:git:repo-1",
			AcceptanceUnitID: "repository:r_payments",
			SourceRunID:      "run-001",
			GenerationID:     "gen-001",
			AcceptedAt:       now,
			UpdatedAt:        now,
		},
	}

	if err := store.Upsert(ctx, rows); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	gotGeneration, gotFound, err := store.Lookup(ctx, "scope:git:repo-1", "repository:r_payments", "run-001")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !gotFound {
		t.Fatal("Lookup should find the inserted row")
	}
	if gotGeneration != "gen-001" {
		t.Fatalf("generation = %q, want %q", gotGeneration, "gen-001")
	}
}

func TestSharedProjectionAcceptanceStoreLookupByUnitReturnsNewest(t *testing.T) {
	t.Parallel()

	db := newSharedProjectionAcceptanceTestDB()
	store := NewSharedProjectionAcceptanceStore(db)
	ctx := context.Background()

	oldTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	newTime := oldTime.Add(30 * time.Minute)
	rows := []SharedProjectionAcceptance{
		{
			ScopeID:          "scope:git:repo-1",
			AcceptanceUnitID: "repository:r_payments",
			SourceRunID:      "run-001",
			GenerationID:     "gen-old",
			AcceptedAt:       oldTime,
			UpdatedAt:        oldTime,
		},
		{
			ScopeID:          "scope:git:repo-2",
			AcceptanceUnitID: "repository:r_payments",
			SourceRunID:      "run-001",
			GenerationID:     "gen-new",
			AcceptedAt:       newTime,
			UpdatedAt:        newTime,
		},
	}

	if err := store.Upsert(ctx, rows); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	gotGeneration, gotFound, err := store.LookupByAcceptanceUnit(ctx, "repository:r_payments", "run-001")
	if err != nil {
		t.Fatalf("LookupByAcceptanceUnit: %v", err)
	}
	if !gotFound {
		t.Fatal("LookupByAcceptanceUnit should find the newest row")
	}
	if gotGeneration != "gen-new" {
		t.Fatalf("generation = %q, want %q", gotGeneration, "gen-new")
	}
}

func TestSharedProjectionAcceptanceStoreLookupNotFound(t *testing.T) {
	t.Parallel()

	db := newSharedProjectionAcceptanceTestDB()
	store := NewSharedProjectionAcceptanceStore(db)

	gotGeneration, gotFound, err := store.Lookup(context.Background(), "scope:none", "unit:none", "run-none")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if gotGeneration != "" {
		t.Fatalf("generation = %q, want empty string", gotGeneration)
	}
	if gotFound {
		t.Fatal("found = true, want false")
	}
}

func TestSharedProjectionAcceptanceSchemaSQL(t *testing.T) {
	t.Parallel()

	sqlStr := SharedProjectionAcceptanceSchemaSQL()
	if !strings.Contains(sqlStr, "CREATE TABLE IF NOT EXISTS shared_projection_acceptance") {
		t.Fatal("missing shared_projection_acceptance table")
	}
	if !strings.Contains(sqlStr, "PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id)") {
		t.Fatal("missing bounded-unit primary key")
	}
	if !strings.Contains(sqlStr, "shared_projection_acceptance_scope_idx") {
		t.Fatal("missing scope index")
	}
	if !strings.Contains(sqlStr, "shared_projection_acceptance_updated_idx") {
		t.Fatal("missing updated_at index")
	}
}

type sharedProjectionAcceptanceRow struct {
	scopeID          string
	acceptanceUnitID string
	sourceRunID      string
	generationID     string
	acceptedAt       time.Time
	updatedAt        time.Time
}

type sharedProjectionAcceptanceTestDB struct {
	rows      map[string]sharedProjectionAcceptanceRow
	execCalls int
}

func newSharedProjectionAcceptanceTestDB() *sharedProjectionAcceptanceTestDB {
	return &sharedProjectionAcceptanceTestDB{
		rows: make(map[string]sharedProjectionAcceptanceRow),
	}
}

func (db *sharedProjectionAcceptanceTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execCalls++

	switch {
	case strings.Contains(query, "INSERT INTO shared_projection_acceptance"):
		const columnsPerRow = 6
		numRows := len(args) / columnsPerRow
		for i := 0; i < numRows; i++ {
			offset := i * columnsPerRow
			row := sharedProjectionAcceptanceRow{
				scopeID:          args[offset+0].(string),
				acceptanceUnitID: args[offset+1].(string),
				sourceRunID:      args[offset+2].(string),
				generationID:     args[offset+3].(string),
				acceptedAt:       args[offset+4].(time.Time),
				updatedAt:        args[offset+5].(time.Time),
			}
			db.rows[acceptanceKey(row.scopeID, row.acceptanceUnitID, row.sourceRunID)] = row
		}
		return sharedIntentResult{}, nil

	case strings.Contains(query, "CREATE TABLE") || strings.Contains(query, "CREATE INDEX"):
		return sharedIntentResult{}, nil

	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *sharedProjectionAcceptanceTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	rows := make([]sharedProjectionAcceptanceRow, 0, len(db.rows))
	for _, row := range db.rows {
		rows = append(rows, row)
	}
	return queryAcceptanceRows(rows, query, args...)
}

func queryAcceptanceRows(rows []sharedProjectionAcceptanceRow, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "WHERE scope_id = $1"):
		if len(args) != 3 {
			return nil, fmt.Errorf("expected 3 args, got %d", len(args))
		}
		scopeID := args[0].(string)
		acceptanceUnitID := args[1].(string)
		sourceRunID := args[2].(string)

		var matches [][]any
		for _, row := range rows {
			if row.scopeID == scopeID &&
				row.acceptanceUnitID == acceptanceUnitID &&
				row.sourceRunID == sourceRunID {
				matches = append(matches, []any{row.generationID})
			}
		}
		return &acceptanceRows{data: matches, idx: -1}, nil

	case strings.Contains(query, "WHERE acceptance_unit_id = $1"):
		if len(args) != 2 {
			return nil, fmt.Errorf("expected 2 args, got %d", len(args))
		}
		acceptanceUnitID := args[0].(string)
		sourceRunID := args[1].(string)

		var newest *sharedProjectionAcceptanceRow
		for _, row := range rows {
			if row.acceptanceUnitID != acceptanceUnitID || row.sourceRunID != sourceRunID {
				continue
			}
			if newest == nil || row.updatedAt.After(newest.updatedAt) {
				candidate := row
				newest = &candidate
			}
		}
		if newest == nil {
			return &acceptanceRows{idx: -1}, nil
		}
		return &acceptanceRows{data: [][]any{{newest.generationID}}, idx: -1}, nil

	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

type acceptanceRows struct {
	data [][]any
	idx  int
}

func (r *acceptanceRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *acceptanceRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	row := r.data[r.idx]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: got %d dest, have %d cols", len(dest), len(row))
	}
	for i, val := range row {
		switch d := dest[i].(type) {
		case *string:
			*d = val.(string)
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *acceptanceRows) Err() error   { return nil }
func (r *acceptanceRows) Close() error { return nil }

func acceptanceKey(scopeID, acceptanceUnitID, sourceRunID string) string {
	return scopeID + "|" + acceptanceUnitID + "|" + sourceRunID
}
