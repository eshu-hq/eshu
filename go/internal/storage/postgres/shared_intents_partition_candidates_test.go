package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSharedIntentStoreListPendingDomainPartitionIntents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, time.June, 16, 9, 0, 0, 0, time.UTC)
	partitionCount := 4
	selectedKey := "code-calls:v1:files:repo-a:src/caller.go"
	selectedPartition := mustPostgresTestPartitionForKey(t, selectedKey, partitionCount)
	otherKey := selectedKey + "-other"
	if mustPostgresTestPartitionForKey(t, otherKey, partitionCount) == selectedPartition {
		otherKey = "code-calls:v1:files:repo-a:src/other_partition.go"
	}
	db := &partitionCandidateListTestDB{rows: []partitionCandidateListRow{
		{
			Intent: reducer.SharedProjectionIntentRow{
				IntentID:         "same-partition",
				ProjectionDomain: reducer.DomainCodeCalls,
				PartitionKey:     selectedKey,
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now,
			},
			HasPartitionHash: true,
			PartitionHash:    reducer.PartitionHashForKey(selectedKey),
		},
		{
			Intent: reducer.SharedProjectionIntentRow{
				IntentID:         "other-partition",
				ProjectionDomain: reducer.DomainCodeCalls,
				PartitionKey:     otherKey,
				ScopeID:          "scope-b",
				AcceptanceUnitID: "repo-b",
				RepositoryID:     "repo-b",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now.Add(time.Second),
			},
			HasPartitionHash: true,
			PartitionHash:    reducer.PartitionHashForKey(otherKey),
		},
	}}
	store := NewSharedIntentStore(db)

	got, err := store.ListPendingDomainPartitionIntents(
		ctx,
		reducer.DomainCodeCalls,
		selectedPartition,
		partitionCount,
		10,
	)
	if err != nil {
		t.Fatalf("ListPendingDomainPartitionIntents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1: %#v", len(got), got)
	}
	if got[0].IntentID != "same-partition" {
		t.Fatalf("IntentID = %q, want same-partition", got[0].IntentID)
	}
	for _, want := range []string{
		"projection_domain = $1",
		"partition_hash IS NOT NULL",
		"mod(partition_hash, $3::numeric) = $2::numeric",
		"completed_at IS NULL",
		"ORDER BY created_at ASC",
		"is_refresh_intent DESC",
		"intent_id ASC",
	} {
		if !strings.Contains(db.query, want) {
			t.Fatalf("query missing %q:\n%s", want, db.query)
		}
	}
}

func TestSharedIntentStoreListPendingDomainUnhashedIntents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, time.June, 16, 9, 15, 0, 0, time.UTC)
	db := &partitionCandidateListTestDB{rows: []partitionCandidateListRow{
		{
			Intent: reducer.SharedProjectionIntentRow{
				IntentID:         "legacy-unhashed",
				ProjectionDomain: reducer.DomainCodeCalls,
				PartitionKey:     "code-calls:v1:files:repo-a:src/legacy.go",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now,
			},
		},
		{
			Intent: reducer.SharedProjectionIntentRow{
				IntentID:         "hashed",
				ProjectionDomain: reducer.DomainCodeCalls,
				PartitionKey:     "code-calls:v1:files:repo-b:src/hashed.go",
				ScopeID:          "scope-b",
				AcceptanceUnitID: "repo-b",
				RepositoryID:     "repo-b",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now.Add(time.Second),
			},
			HasPartitionHash: true,
			PartitionHash:    reducer.PartitionHashForKey("code-calls:v1:files:repo-b:src/hashed.go"),
		},
	}}
	store := NewSharedIntentStore(db)

	got, err := store.ListPendingDomainUnhashedIntents(ctx, reducer.DomainCodeCalls, 10)
	if err != nil {
		t.Fatalf("ListPendingDomainUnhashedIntents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1: %#v", len(got), got)
	}
	if got[0].IntentID != "legacy-unhashed" {
		t.Fatalf("IntentID = %q, want legacy-unhashed", got[0].IntentID)
	}
	for _, want := range []string{
		"projection_domain = $1",
		"partition_hash IS NULL",
		"completed_at IS NULL",
		"ORDER BY created_at ASC, intent_id ASC",
	} {
		if !strings.Contains(db.query, want) {
			t.Fatalf("query missing %q:\n%s", want, db.query)
		}
	}
}

type partitionCandidateListRow struct {
	Intent           reducer.SharedProjectionIntentRow
	HasPartitionHash bool
	PartitionHash    uint64
}

type partitionCandidateListTestDB struct {
	query string
	args  []any
	rows  []partitionCandidateListRow
}

func (db *partitionCandidateListTestDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected exec")
}

func (db *partitionCandidateListTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.query = query
	db.args = append([]any(nil), args...)
	domain := args[0].(string)
	unhashedOnly := strings.Contains(query, "partition_hash IS NULL")
	limit := args[len(args)-1].(int)
	rows := make([]reducer.SharedProjectionIntentRow, 0, len(db.rows))
	for _, candidate := range db.rows {
		if candidate.Intent.ProjectionDomain != domain || candidate.Intent.CompletedAt != nil {
			continue
		}
		if unhashedOnly {
			if candidate.HasPartitionHash {
				continue
			}
			rows = append(rows, candidate.Intent)
			continue
		}
		partitionID := args[1].(int)
		partitionCount := args[2].(int)
		if !candidate.HasPartitionHash || int(candidate.PartitionHash%uint64(partitionCount)) != partitionID {
			continue
		}
		rows = append(rows, candidate.Intent)
	}
	// Mirror the index-backed SQL sort:
	// ORDER BY created_at ASC, is_refresh_intent DESC, intent_id ASC
	// is_refresh_intent is a stored generated BOOLEAN column; DESC puts true
	// (refresh rows) before false (upsert rows) at the same timestamp.
	refreshPriority := func(row reducer.SharedProjectionIntentRow) int {
		if action, _ := row.Payload["action"].(string); action == "refresh" {
			return 0
		}
		return 1
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		}
		pi, pj := refreshPriority(rows[i]), refreshPriority(rows[j])
		if pi != pj {
			return pi < pj
		}
		return rows[i].IntentID < rows[j].IntentID
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return &partitionCandidateRows{rows: rows, idx: -1}, nil
}

type partitionCandidateRows struct {
	rows []reducer.SharedProjectionIntentRow
	idx  int
}

func (r *partitionCandidateRows) Next() bool {
	r.idx++
	return r.idx < len(r.rows)
}

func (r *partitionCandidateRows) Scan(dest ...any) error {
	row := r.rows[r.idx]
	payload, err := json.Marshal(row.Payload)
	if err != nil {
		return err
	}
	values := []any{
		row.IntentID,
		row.ProjectionDomain,
		row.PartitionKey,
		row.ScopeID,
		row.AcceptanceUnitID,
		row.RepositoryID,
		row.SourceRunID,
		row.GenerationID,
		payload,
		row.CreatedAt,
		sql.NullTime{},
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = values[i].(string)
		case *[]byte:
			*d = values[i].([]byte)
		case *time.Time:
			*d = values[i].(time.Time)
		case *sql.NullTime:
			*d = values[i].(sql.NullTime)
		default:
			return fmt.Errorf("unsupported dest %T", dest[i])
		}
	}
	return nil
}

func (r *partitionCandidateRows) Err() error {
	return nil
}

func (r *partitionCandidateRows) Close() error {
	return nil
}

// TestListPendingDomainPartitionIntentsRefreshFirst verifies that refresh
// intents sort before upsert intents at the same created_at timestamp.
//
// Regression: with >batchLimit upsert rows sharing the same created_at as the
// refresh row, the refresh row was pushed to position >batchLimit and never
// entered a processing batch.  The repo-wide retract fence never opened, so
// all per-edge upsert rows deferred indefinitely (#3451).
func TestListPendingDomainPartitionIntentsRefreshFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// All rows share the same created_at so that the tie-break order is the
	// only thing that determines position in the batch.
	now := time.Date(2026, time.June, 21, 1, 43, 1, 0, time.UTC)

	refreshKey := "inheritance_edges:refresh:v1:whole:repository:r_abc123"
	refreshPartition := mustPostgresTestPartitionForKey(t, refreshKey, 8)

	// Build batchLimit+1 upsert rows that hash to the same partition as the
	// refresh row.  The batch limit is 200 in production; use 3 here so the
	// test is fast.  The invariant being checked is ordering, not size.
	const batchLimit = 3
	rows := make([]partitionCandidateListRow, 0, batchLimit+2)

	// Refresh row — same timestamp as the upsert rows.
	rows = append(rows, partitionCandidateListRow{
		Intent: reducer.SharedProjectionIntentRow{
			IntentID:         "refresh-row",
			ProjectionDomain: reducer.DomainInheritanceEdges,
			PartitionKey:     refreshKey,
			ScopeID:          "scope-a",
			AcceptanceUnitID: "au-a",
			RepositoryID:     "repo-abc123",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Payload:          map[string]any{"action": "refresh", "repo_id": "repository:r_abc123"},
			CreatedAt:        now,
		},
		HasPartitionHash: true,
		PartitionHash:    reducer.PartitionHashForKey(refreshKey),
	})

	// batchLimit upsert rows that also hash to refreshPartition, same timestamp.
	// Without the fix, they fill the batch and push refresh to position
	// batchLimit+1.
	for i := range batchLimit {
		upsertKey := fmt.Sprintf("inheritance-edges:v1:files:repository:r_abc123:file%d.go", i)
		// Force same partition as refresh row.
		if int(reducer.PartitionHashForKey(upsertKey)%8) != refreshPartition {
			// Adjust key until it lands on the right partition.
			for j := range 1000 {
				candidate := fmt.Sprintf("inheritance-edges:v1:files:repository:r_abc123:file%d_%d.go", i, j)
				if int(reducer.PartitionHashForKey(candidate)%8) == refreshPartition {
					upsertKey = candidate
					break
				}
			}
		}
		rows = append(rows, partitionCandidateListRow{
			Intent: reducer.SharedProjectionIntentRow{
				IntentID:         fmt.Sprintf("upsert-%d", i),
				ProjectionDomain: reducer.DomainInheritanceEdges,
				PartitionKey:     upsertKey,
				ScopeID:          "scope-a",
				AcceptanceUnitID: "au-a",
				RepositoryID:     "repo-abc123",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"action": "upsert"},
				CreatedAt:        now,
			},
			HasPartitionHash: true,
			PartitionHash:    reducer.PartitionHashForKey(upsertKey),
		})
	}

	db := &partitionCandidateListTestDB{rows: rows}
	store := NewSharedIntentStore(db)

	got, err := store.ListPendingDomainPartitionIntents(
		ctx,
		reducer.DomainInheritanceEdges,
		refreshPartition,
		8,
		batchLimit, // batch is exactly full — refresh must be first to appear
	)
	if err != nil {
		t.Fatalf("ListPendingDomainPartitionIntents: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("got 0 rows, want at least 1")
	}
	// The refresh row must be the first element returned; with the fixed
	// ORDER BY it is position 1, so it is always within any batch.
	if got[0].IntentID != "refresh-row" {
		t.Fatalf("first row IntentID = %q, want %q (refresh must sort before upsert at same timestamp)", got[0].IntentID, "refresh-row")
	}
	// The SQL must ORDER BY the generated column is_refresh_intent DESC so
	// the planner can use the stored-column index instead of evaluating the
	// JSONB expression or performing a full sort on large pending backlogs.
	for _, want := range []string{
		"is_refresh_intent DESC",
	} {
		if !strings.Contains(db.query, want) {
			t.Fatalf("query missing index-backed refresh-priority sort %q:\n%s", want, db.query)
		}
	}
}

func mustPostgresTestPartitionForKey(t *testing.T, key string, partitionCount int) int {
	t.Helper()
	partitionID, err := reducer.PartitionForKey(key, partitionCount)
	if err != nil {
		t.Fatalf("PartitionForKey(%q, %d): %v", key, partitionCount, err)
	}
	return partitionID
}
