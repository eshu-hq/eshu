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

func TestGraphProjectionPhaseRepairQueueStoreEnqueueAndListDue(t *testing.T) {
	t.Parallel()

	db := newGraphProjectionPhaseRepairQueueTestDB()
	store := NewGraphProjectionPhaseRepairQueueStore(db)
	now := time.Date(2026, time.April, 17, 11, 0, 0, 0, time.UTC)
	repair := reducer.GraphProjectionPhaseRepair{
		Key: reducer.GraphProjectionPhaseKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		},
		Phase:         reducer.GraphProjectionPhaseSemanticNodesCommitted,
		CommittedAt:   now.Add(-time.Minute),
		EnqueuedAt:    now,
		NextAttemptAt: now,
		LastError:     "publish failed",
	}

	if err := store.Enqueue(context.Background(), []reducer.GraphProjectionPhaseRepair{repair}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	repairs, err := store.ListDue(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("ListDue() error = %v", err)
	}
	if len(repairs) != 1 {
		t.Fatalf("len(repairs) = %d, want 1", len(repairs))
	}
	if got, want := repairs[0].Phase, reducer.GraphProjectionPhaseSemanticNodesCommitted; got != want {
		t.Fatalf("repairs[0].Phase = %q, want %q", got, want)
	}
}

func TestGraphProjectionPhaseRepairQueueStoreDelete(t *testing.T) {
	t.Parallel()

	db := newGraphProjectionPhaseRepairQueueTestDB()
	store := NewGraphProjectionPhaseRepairQueueStore(db)
	now := time.Date(2026, time.April, 17, 11, 0, 0, 0, time.UTC)
	repair := reducer.GraphProjectionPhaseRepair{
		Key: reducer.GraphProjectionPhaseKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		},
		Phase:         reducer.GraphProjectionPhaseSemanticNodesCommitted,
		CommittedAt:   now.Add(-time.Minute),
		EnqueuedAt:    now,
		NextAttemptAt: now,
	}

	if err := store.Enqueue(context.Background(), []reducer.GraphProjectionPhaseRepair{repair}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := store.Delete(context.Background(), []reducer.GraphProjectionPhaseRepair{repair}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	repairs, err := store.ListDue(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("ListDue() error = %v", err)
	}
	if len(repairs) != 0 {
		t.Fatalf("len(repairs) = %d, want 0", len(repairs))
	}
}

func TestGraphProjectionPhaseRepairQueueStoreMarkFailed(t *testing.T) {
	t.Parallel()

	db := newGraphProjectionPhaseRepairQueueTestDB()
	store := NewGraphProjectionPhaseRepairQueueStore(db)
	now := time.Date(2026, time.April, 17, 11, 0, 0, 0, time.UTC)
	repair := reducer.GraphProjectionPhaseRepair{
		Key: reducer.GraphProjectionPhaseKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		},
		Phase:         reducer.GraphProjectionPhaseSemanticNodesCommitted,
		CommittedAt:   now.Add(-time.Minute),
		EnqueuedAt:    now,
		NextAttemptAt: now,
	}

	if err := store.Enqueue(context.Background(), []reducer.GraphProjectionPhaseRepair{repair}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	nextAttemptAt := now.Add(5 * time.Minute)
	if err := store.MarkFailed(context.Background(), repair, nextAttemptAt, "still failed"); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}

	repairs, err := store.ListDue(context.Background(), nextAttemptAt.Add(-time.Second), 10)
	if err != nil {
		t.Fatalf("ListDue() early error = %v", err)
	}
	if len(repairs) != 0 {
		t.Fatalf("len(repairs) before next attempt = %d, want 0", len(repairs))
	}

	repairs, err = store.ListDue(context.Background(), nextAttemptAt, 10)
	if err != nil {
		t.Fatalf("ListDue() after delay error = %v", err)
	}
	if len(repairs) != 1 {
		t.Fatalf("len(repairs) after delay = %d, want 1", len(repairs))
	}
	if got, want := repairs[0].Attempts, 1; got != want {
		t.Fatalf("repairs[0].Attempts = %d, want %d", got, want)
	}
	if got, want := repairs[0].LastError, "still failed"; got != want {
		t.Fatalf("repairs[0].LastError = %q, want %q", got, want)
	}
}

type graphProjectionPhaseRepairQueueRow struct {
	scopeID          string
	acceptanceUnitID string
	sourceRunID      string
	generationID     string
	keyspace         string
	phase            string
	committedAt      time.Time
	enqueuedAt       time.Time
	nextAttemptAt    time.Time
	attempts         int
	lastError        string
	updatedAt        time.Time
}

type graphProjectionPhaseRepairQueueTestDB struct {
	rows map[string]graphProjectionPhaseRepairQueueRow
}

func newGraphProjectionPhaseRepairQueueTestDB() *graphProjectionPhaseRepairQueueTestDB {
	return &graphProjectionPhaseRepairQueueTestDB{
		rows: make(map[string]graphProjectionPhaseRepairQueueRow),
	}
}

func (db *graphProjectionPhaseRepairQueueTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO graph_projection_phase_repair_queue"):
		const columnsPerRow = 11
		for i := 0; i < len(args); i += columnsPerRow {
			row := graphProjectionPhaseRepairQueueRow{
				scopeID:          args[i+0].(string),
				acceptanceUnitID: args[i+1].(string),
				sourceRunID:      args[i+2].(string),
				generationID:     args[i+3].(string),
				keyspace:         args[i+4].(string),
				phase:            args[i+5].(string),
				committedAt:      args[i+6].(time.Time),
				enqueuedAt:       args[i+7].(time.Time),
				nextAttemptAt:    args[i+8].(time.Time),
				attempts:         args[i+9].(int),
				lastError:        args[i+10].(string),
				updatedAt:        args[i+7].(time.Time),
			}
			db.rows[graphProjectionPhaseRepairQueueCompositeKey(row)] = row
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "DELETE FROM graph_projection_phase_repair_queue"):
		for i := 0; i < len(args); i += 6 {
			key := graphProjectionPhaseRepairQueueCompositeKey(graphProjectionPhaseRepairQueueRow{
				scopeID:          args[i+0].(string),
				acceptanceUnitID: args[i+1].(string),
				sourceRunID:      args[i+2].(string),
				generationID:     args[i+3].(string),
				keyspace:         args[i+4].(string),
				phase:            args[i+5].(string),
			})
			delete(db.rows, key)
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "UPDATE graph_projection_phase_repair_queue"):
		key := graphProjectionPhaseRepairQueueCompositeKey(graphProjectionPhaseRepairQueueRow{
			scopeID:          args[3].(string),
			acceptanceUnitID: args[4].(string),
			sourceRunID:      args[5].(string),
			generationID:     args[6].(string),
			keyspace:         args[7].(string),
			phase:            args[8].(string),
		})
		row := db.rows[key]
		row.nextAttemptAt = args[0].(time.Time)
		row.lastError = args[1].(string)
		row.updatedAt = args[2].(time.Time)
		row.attempts++
		db.rows[key] = row
		return sharedIntentResult{}, nil
	case strings.Contains(query, "CREATE TABLE") || strings.Contains(query, "CREATE INDEX"):
		return sharedIntentResult{}, nil
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *graphProjectionPhaseRepairQueueTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "FROM graph_projection_phase_repair_queue"):
		now := args[0].(time.Time)
		limit := args[1].(int)
		rows := make([]graphProjectionPhaseRepairQueueRow, 0, len(db.rows))
		for _, row := range db.rows {
			if row.nextAttemptAt.After(now) {
				continue
			}
			rows = append(rows, row)
		}
		if len(rows) > limit {
			rows = rows[:limit]
		}
		return &graphProjectionPhaseRepairQueueRows{data: rows, idx: -1}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func graphProjectionPhaseRepairQueueCompositeKey(row graphProjectionPhaseRepairQueueRow) string {
	return strings.Join([]string{
		row.scopeID,
		row.acceptanceUnitID,
		row.sourceRunID,
		row.generationID,
		row.keyspace,
		row.phase,
	}, "|")
}

type graphProjectionPhaseRepairQueueRows struct {
	data []graphProjectionPhaseRepairQueueRow
	idx  int
}

func (r *graphProjectionPhaseRepairQueueRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *graphProjectionPhaseRepairQueueRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	if len(dest) != 11 {
		return fmt.Errorf("scan: got %d dest, want 11", len(dest))
	}
	row := r.data[r.idx]
	*(dest[0].(*string)) = row.scopeID
	*(dest[1].(*string)) = row.acceptanceUnitID
	*(dest[2].(*string)) = row.sourceRunID
	*(dest[3].(*string)) = row.generationID
	*(dest[4].(*reducer.GraphProjectionKeyspace)) = reducer.GraphProjectionKeyspace(row.keyspace)
	*(dest[5].(*reducer.GraphProjectionPhase)) = reducer.GraphProjectionPhase(row.phase)
	*(dest[6].(*time.Time)) = row.committedAt
	*(dest[7].(*time.Time)) = row.enqueuedAt
	*(dest[8].(*time.Time)) = row.nextAttemptAt
	*(dest[9].(*int)) = row.attempts
	*(dest[10].(*string)) = row.lastError
	return nil
}

func (r *graphProjectionPhaseRepairQueueRows) Err() error   { return nil }
func (r *graphProjectionPhaseRepairQueueRows) Close() error { return nil }
