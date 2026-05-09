package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestStatusStoreReadRawSnapshot(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"active", int64(5)},
					{"pending", int64(1)},
					{"completed", int64(4)},
					{"superseded", int64(3)},
					{"failed", int64(1)},
					{"inactive", int64(2)},
				},
			},
			{
				rows: [][]any{
					{"active", int64(5)},
					{"pending", int64(2)},
					{"completed", int64(4)},
					{"superseded", int64(3)},
					{"failed", int64(1)},
					{"inactive", int64(2)},
				},
			},
			{
				rows: [][]any{
					{"scope-1", "generation-b", "active", "snapshot", "fresh snapshot", time.Date(2026, 4, 12, 15, 45, 0, 0, time.UTC), time.Date(2026, 4, 12, 15, 46, 0, 0, time.UTC), nil, "generation-b"},
					{"scope-1", "generation-a", "superseded", "snapshot", "changed files", time.Date(2026, 4, 12, 15, 30, 0, 0, time.UTC), time.Date(2026, 4, 12, 15, 31, 0, 0, time.UTC), time.Date(2026, 4, 12, 15, 40, 0, 0, time.UTC), "generation-b"},
				},
			},
			{
				rows: [][]any{
					{"projector", "pending", int64(2)},
					{"projector", "running", int64(1)},
					{"reducer", "retrying", int64(1)},
				},
			},
			{
				rows: [][]any{
					{"repository", int64(3), int64(2), int64(1), int64(0), int64(0), 90.0},
					{"shared-platform", int64(1), int64(1), int64(0), int64(1), int64(0), 30.0},
				},
			},
			{
				rows: [][]any{
					{int64(9), int64(4), int64(1), int64(2), int64(1), int64(3), int64(1), int64(0), 90.0, int64(0)},
				},
			},
			{
				rows: [][]any{
					{"reducer", "semantic_entity_materialization", "code_graph", "scope-1:generation-b:code", int64(2), 75.0},
				},
			},
			{
				rows: [][]any{
					{
						"reducer",
						"code_call_materialization",
						"retrying",
						"work-1",
						"scope-1",
						"generation-b",
						"graph_write_timeout",
						"neo4j execute group timed out after 2s",
						"phase=semantic label=Variable rows=500",
						time.Date(2026, 4, 12, 15, 59, 0, 0, time.UTC),
					},
				},
			},
		},
	}

	store := NewStatusStore(queryer)
	got, err := store.ReadRawSnapshot(context.Background(), time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ReadRawSnapshot() error = %v, want nil", err)
	}

	wantQueue := statuspkg.QueueSnapshot{
		Total:                9,
		Outstanding:          4,
		Pending:              1,
		InFlight:             2,
		Retrying:             1,
		Succeeded:            3,
		Failed:               0,
		DeadLetter:           1,
		OldestOutstandingAge: 90 * time.Second,
		OverdueClaims:        0,
	}
	if got.Queue != wantQueue {
		t.Fatalf("ReadRawSnapshot().Queue = %#v, want %#v", got.Queue, wantQueue)
	}
	if got.LatestQueueFailure == nil {
		t.Fatal("ReadRawSnapshot().LatestQueueFailure = nil, want latest failure")
	}
	if got.LatestQueueFailure.FailureClass != "graph_write_timeout" {
		t.Fatalf("ReadRawSnapshot().LatestQueueFailure.FailureClass = %q, want graph_write_timeout", got.LatestQueueFailure.FailureClass)
	}
	if got.LatestQueueFailure.FailureDetails != "phase=semantic label=Variable rows=500" {
		t.Fatalf("ReadRawSnapshot().LatestQueueFailure.FailureDetails = %q, want graph write details", got.LatestQueueFailure.FailureDetails)
	}
	if len(got.ScopeCounts) != 6 {
		t.Fatalf("ReadRawSnapshot().ScopeCounts len = %d, want 6", len(got.ScopeCounts))
	}
	if got.ScopeActivity.Active != 5 {
		t.Fatalf("ReadRawSnapshot().ScopeActivity.Active = %d, want 5", got.ScopeActivity.Active)
	}
	if got.ScopeActivity.Changed != 2 {
		t.Fatalf("ReadRawSnapshot().ScopeActivity.Changed = %d, want 2", got.ScopeActivity.Changed)
	}
	if got.ScopeActivity.Unchanged != 3 {
		t.Fatalf("ReadRawSnapshot().ScopeActivity.Unchanged = %d, want 3", got.ScopeActivity.Unchanged)
	}
	if got.GenerationHistory.Active != 5 {
		t.Fatalf("ReadRawSnapshot().GenerationHistory.Active = %d, want 5", got.GenerationHistory.Active)
	}
	if got.GenerationHistory.Pending != 2 {
		t.Fatalf("ReadRawSnapshot().GenerationHistory.Pending = %d, want 2", got.GenerationHistory.Pending)
	}
	if got.GenerationHistory.Completed != 4 {
		t.Fatalf("ReadRawSnapshot().GenerationHistory.Completed = %d, want 4", got.GenerationHistory.Completed)
	}
	if got.GenerationHistory.Superseded != 3 {
		t.Fatalf("ReadRawSnapshot().GenerationHistory.Superseded = %d, want 3", got.GenerationHistory.Superseded)
	}
	if got.GenerationHistory.Failed != 1 {
		t.Fatalf("ReadRawSnapshot().GenerationHistory.Failed = %d, want 1", got.GenerationHistory.Failed)
	}
	if got.GenerationHistory.Other != 2 {
		t.Fatalf("ReadRawSnapshot().GenerationHistory.Other = %d, want 2", got.GenerationHistory.Other)
	}
	if len(got.StageCounts) != 3 {
		t.Fatalf("ReadRawSnapshot().StageCounts len = %d, want 3", len(got.StageCounts))
	}
	if len(got.GenerationTransitions) != 2 {
		t.Fatalf("ReadRawSnapshot().GenerationTransitions len = %d, want 2", len(got.GenerationTransitions))
	}
	if got.GenerationTransitions[0].CurrentActiveGenerationID != "generation-b" {
		t.Fatalf("ReadRawSnapshot().GenerationTransitions[0].CurrentActiveGenerationID = %q, want %q", got.GenerationTransitions[0].CurrentActiveGenerationID, "generation-b")
	}
	if len(got.DomainBacklogs) != 2 {
		t.Fatalf("ReadRawSnapshot().DomainBacklogs len = %d, want 2", len(got.DomainBacklogs))
	}
	if got.DomainBacklogs[0].OldestAge != 90*time.Second {
		t.Fatalf("ReadRawSnapshot().DomainBacklogs[0].OldestAge = %v, want %v", got.DomainBacklogs[0].OldestAge, 90*time.Second)
	}
	if got.DomainBacklogs[0].InFlight != 2 {
		t.Fatalf("ReadRawSnapshot().DomainBacklogs[0].InFlight = %d, want 2", got.DomainBacklogs[0].InFlight)
	}
	if len(got.QueueBlockages) != 1 {
		t.Fatalf("ReadRawSnapshot().QueueBlockages len = %d, want 1", len(got.QueueBlockages))
	}
	if got.QueueBlockages[0].ConflictKey != "scope-1:generation-b:code" {
		t.Fatalf("ReadRawSnapshot().QueueBlockages[0].ConflictKey = %q, want conflict key", got.QueueBlockages[0].ConflictKey)
	}
	if got.QueueBlockages[0].OldestAge != 75*time.Second {
		t.Fatalf("ReadRawSnapshot().QueueBlockages[0].OldestAge = %v, want 75s", got.QueueBlockages[0].OldestAge)
	}
	if got.Coordinator != nil {
		t.Fatalf("ReadRawSnapshot().Coordinator = %#v, want nil", got.Coordinator)
	}

	if len(queryer.queries) != 13 {
		t.Fatalf("QueryContext() call count = %d, want 13", len(queryer.queries))
	}
	for _, want := range []string{
		"FROM ingestion_scopes",
		"FROM scope_generations",
		"JOIN ingestion_scopes",
		"activated_at",
		"superseded_at",
		"FROM fact_work_items",
		"inflight.conflict_domain",
		"failure_details",
	} {
		joined := strings.Join(queryer.queries, "\n")
		if !strings.Contains(joined, want) {
			t.Fatalf("queries missing %q:\n%s", want, joined)
		}
	}
}

func TestStatusStoreReadRawSnapshotPropagatesQueryErrors(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{err: errors.New("boom")},
		},
	}

	store := NewStatusStore(queryer)
	_, err := store.ReadRawSnapshot(context.Background(), time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("ReadRawSnapshot() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "list scope counts") {
		t.Fatalf("ReadRawSnapshot() error = %q, want prefix context", err)
	}
}

func TestStatusQueriesUseAggregateFilterSyntax(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"domainBacklogQuery": domainBacklogQuery,
		"queueSnapshotQuery": queueSnapshotQuery,
	} {
		if !strings.Contains(query, "MIN(created_at)") || !strings.Contains(query, "FILTER") {
			t.Fatalf("%s missing aggregate FILTER placement:\n%s", name, query)
		}
		if strings.Contains(query, "EXTRACT(EPOCH FROM ($1 - MIN(created_at)))\n           FILTER") {
			t.Fatalf("%s uses invalid FILTER placement:\n%s", name, query)
		}
	}
	if !strings.Contains(domainBacklogQuery, "FROM shared_projection_intents") {
		t.Fatalf("domainBacklogQuery missing shared projection intent backlog:\n%s", domainBacklogQuery)
	}
	if !strings.Contains(domainBacklogQuery, "completed_at IS NULL") {
		t.Fatalf("domainBacklogQuery missing pending shared projection filter:\n%s", domainBacklogQuery)
	}
	if !strings.Contains(domainBacklogQuery, "FROM shared_projection_partition_leases") {
		t.Fatalf("domainBacklogQuery missing active shared projection lease source:\n%s", domainBacklogQuery)
	}
	if !strings.Contains(domainBacklogQuery, "lease_expires_at > $1") {
		t.Fatalf("domainBacklogQuery missing active shared projection lease expiry check:\n%s", domainBacklogQuery)
	}
}

func TestLatestQueueFailureQueryIgnoresInFlightRows(t *testing.T) {
	t.Parallel()

	if strings.Contains(latestQueueFailureQuery, "'claimed'") ||
		strings.Contains(latestQueueFailureQuery, "'running'") {
		t.Fatalf("latestQueueFailureQuery should not treat in-flight heartbeats as latest failures:\n%s", latestQueueFailureQuery)
	}
	if !strings.Contains(latestQueueFailureQuery, "status IN ('retrying', 'failed', 'dead_letter')") {
		t.Fatalf("latestQueueFailureQuery missing retry/terminal filter:\n%s", latestQueueFailureQuery)
	}
}

func TestReadCoordinatorSnapshotHandlesNullableDeactivatedAtAndCreatedAtBacklogFallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 20, 15, 45, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{
						"collector-git-default",
						"git",
						"continuous",
						true,
						true,
						false,
						"",
						now.Add(-15 * time.Second),
						now.Add(-5 * time.Second),
						nil,
					},
				},
			},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{
				rows: [][]any{
					{int64(1), int64(0), 42.0},
				},
			},
		},
	}

	got, err := readCoordinatorSnapshot(context.Background(), queryer, now)
	if err != nil {
		t.Fatalf("readCoordinatorSnapshot() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("readCoordinatorSnapshot() = nil, want snapshot")
	}
	if len(got.CollectorInstances) != 1 {
		t.Fatalf("readCoordinatorSnapshot().CollectorInstances len = %d, want 1", len(got.CollectorInstances))
	}
	if !got.CollectorInstances[0].DeactivatedAt.IsZero() {
		t.Fatalf("readCoordinatorSnapshot().CollectorInstances[0].DeactivatedAt = %v, want zero", got.CollectorInstances[0].DeactivatedAt)
	}
	if got.OldestPendingAge != 42*time.Second {
		t.Fatalf("readCoordinatorSnapshot().OldestPendingAge = %v, want %v", got.OldestPendingAge, 42*time.Second)
	}
	if !strings.Contains(workflowCoordinatorClaimSnapshotQuery, "MIN(COALESCE(visible_at, created_at))") {
		t.Fatalf("workflowCoordinatorClaimSnapshotQuery missing created_at fallback:\n%s", workflowCoordinatorClaimSnapshotQuery)
	}
}

type fakeQueryer struct {
	responses []fakeRows
	queries   []string
}

func (q *fakeQueryer) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	q.queries = append(q.queries, query)
	if len(q.responses) == 0 {
		if isWorkflowCoordinatorStatusQuery(query) {
			return &fakeRows{}, nil
		}
		return nil, fmt.Errorf("unexpected query: %s", query)
	}

	rows := q.responses[0]
	q.responses = q.responses[1:]
	if rows.err != nil {
		return nil, rows.err
	}
	return &rows, nil
}

type fakeRows struct {
	rows  [][]any
	err   error
	index int
}

func (r *fakeRows) Next() bool {
	return r.index < len(r.rows)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("scan called without row")
	}
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *bool:
			value, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want bool", i, row[i])
			}
			*target = value
		case *int:
			value, ok := row[i].(int64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int64 for int target", i, row[i])
			}
			*target = int(value)
		case *int64:
			value, ok := row[i].(int64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int64", i, row[i])
			}
			*target = value
		case *float64:
			value, ok := row[i].(float64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want float64", i, row[i])
			}
			*target = value
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want time.Time", i, row[i])
			}
			*target = value
		case *sql.NullTime:
			switch value := row[i].(type) {
			case nil:
				*target = sql.NullTime{}
			case time.Time:
				*target = sql.NullTime{Time: value, Valid: true}
			default:
				return fmt.Errorf("row[%d] type = %T, want time.Time or nil", i, row[i])
			}
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	r.index++
	return nil
}

func (r *fakeRows) Err() error { return nil }

func (r *fakeRows) Close() error { return nil }
