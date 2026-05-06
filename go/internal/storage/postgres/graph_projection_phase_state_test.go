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

func TestGraphProjectionPhaseStateStoreUpsertAndLookup(t *testing.T) {
	t.Parallel()

	db := newGraphProjectionPhaseStateTestDB()
	store := NewGraphProjectionPhaseStateStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	key := reducer.GraphProjectionPhaseKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
	}

	err := store.Upsert(ctx, []reducer.GraphProjectionPhaseState{{
		Key:         key,
		Phase:       reducer.GraphProjectionPhaseSemanticNodesCommitted,
		CommittedAt: now,
		UpdatedAt:   now,
	}})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	ready, found, err := store.Lookup(ctx, key, reducer.GraphProjectionPhaseSemanticNodesCommitted)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if !found {
		t.Fatal("Lookup() found = false, want true")
	}
	if !ready {
		t.Fatal("Lookup() ready = false, want true")
	}
}

func TestGraphProjectionPhaseStateStoreLookupNotFound(t *testing.T) {
	t.Parallel()

	db := newGraphProjectionPhaseStateTestDB()
	store := NewGraphProjectionPhaseStateStore(db)
	key := reducer.GraphProjectionPhaseKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
	}

	ready, found, err := store.Lookup(context.Background(), key, reducer.GraphProjectionPhaseSemanticNodesCommitted)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if ready {
		t.Fatal("Lookup() ready = true, want false")
	}
	if found {
		t.Fatal("Lookup() found = true, want false")
	}
}

func TestGraphProjectionPhaseStateSchemaSQL(t *testing.T) {
	t.Parallel()

	sqlStr := GraphProjectionPhaseStateSchemaSQL()
	if !strings.Contains(sqlStr, "CREATE TABLE IF NOT EXISTS graph_projection_phase_state") {
		t.Fatal("missing graph_projection_phase_state table")
	}
	if !strings.Contains(sqlStr, "PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)") {
		t.Fatal("missing graph projection phase primary key")
	}
	if !strings.Contains(sqlStr, "graph_projection_phase_state_lookup_idx") {
		t.Fatal("missing graph projection lookup index")
	}
}

type graphProjectionPhaseStateRow struct {
	scopeID          string
	acceptanceUnitID string
	sourceRunID      string
	generationID     string
	keyspace         string
	phase            string
	committedAt      time.Time
	updatedAt        time.Time
}

type graphProjectionPhaseStateTestDB struct {
	rows map[string]graphProjectionPhaseStateRow
}

func newGraphProjectionPhaseStateTestDB() *graphProjectionPhaseStateTestDB {
	return &graphProjectionPhaseStateTestDB{
		rows: make(map[string]graphProjectionPhaseStateRow),
	}
}

func (db *graphProjectionPhaseStateTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO graph_projection_phase_state"):
		const columnsPerRow = 8
		for i := 0; i < len(args); i += columnsPerRow {
			row := graphProjectionPhaseStateRow{
				scopeID:          args[i+0].(string),
				acceptanceUnitID: args[i+1].(string),
				sourceRunID:      args[i+2].(string),
				generationID:     args[i+3].(string),
				keyspace:         args[i+4].(string),
				phase:            args[i+5].(string),
				committedAt:      args[i+6].(time.Time),
				updatedAt:        args[i+7].(time.Time),
			}
			db.rows[graphProjectionPhaseStateCompositeKey(row)] = row
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "CREATE TABLE") || strings.Contains(query, "CREATE INDEX"):
		return sharedIntentResult{}, nil
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *graphProjectionPhaseStateTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "FROM graph_projection_phase_state"):
		if len(args) != 6 {
			return nil, fmt.Errorf("expected 6 args, got %d", len(args))
		}
		key := graphProjectionPhaseStateCompositeKey(graphProjectionPhaseStateRow{
			scopeID:          args[0].(string),
			acceptanceUnitID: args[1].(string),
			sourceRunID:      args[2].(string),
			generationID:     args[3].(string),
			keyspace:         args[4].(string),
			phase:            args[5].(string),
		})
		_, ok := db.rows[key]
		if !ok {
			return &graphProjectionBoolRows{idx: -1}, nil
		}
		return &graphProjectionBoolRows{data: []bool{true}, idx: -1}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func graphProjectionPhaseStateCompositeKey(row graphProjectionPhaseStateRow) string {
	return strings.Join([]string{
		row.scopeID,
		row.acceptanceUnitID,
		row.sourceRunID,
		row.generationID,
		row.keyspace,
		row.phase,
	}, "|")
}

type graphProjectionBoolRows struct {
	data []bool
	idx  int
}

func (r *graphProjectionBoolRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *graphProjectionBoolRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	if len(dest) != 1 {
		return fmt.Errorf("scan: got %d dest, want 1", len(dest))
	}
	typed, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("unsupported scan dest type %T", dest[0])
	}
	*typed = r.data[r.idx]
	return nil
}

func (r *graphProjectionBoolRows) Err() error   { return nil }
func (r *graphProjectionBoolRows) Close() error { return nil }
