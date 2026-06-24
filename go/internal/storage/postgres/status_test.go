// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	"github.com/lib/pq"
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
					{true, 30.0},
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
			{
				rows: [][]any{
					{int64(2), int64(1), int64(3), 180.0},
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
	if !got.ProducerActivity.HasActiveOrPendingGeneration {
		t.Fatal("ReadRawSnapshot().ProducerActivity.HasActiveOrPendingGeneration = false, want true")
	}
	if got.ProducerActivity.LatestGenerationAge != 30*time.Second {
		t.Fatalf("ReadRawSnapshot().ProducerActivity.LatestGenerationAge = %v, want 30s", got.ProducerActivity.LatestGenerationAge)
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
	if got.CollectorGenerationDeadLetters.DeadLetter != 2 {
		t.Fatalf("ReadRawSnapshot().CollectorGenerationDeadLetters.DeadLetter = %d, want 2", got.CollectorGenerationDeadLetters.DeadLetter)
	}
	if got.CollectorGenerationDeadLetters.ReplayRequested != 1 {
		t.Fatalf("ReadRawSnapshot().CollectorGenerationDeadLetters.ReplayRequested = %d, want 1", got.CollectorGenerationDeadLetters.ReplayRequested)
	}
	if got.CollectorGenerationDeadLetters.ReplayAttempts != 3 {
		t.Fatalf("ReadRawSnapshot().CollectorGenerationDeadLetters.ReplayAttempts = %d, want 3", got.CollectorGenerationDeadLetters.ReplayAttempts)
	}
	if got.CollectorGenerationDeadLetters.OldestDeadLetterAge != 3*time.Minute {
		t.Fatalf("ReadRawSnapshot().CollectorGenerationDeadLetters.OldestDeadLetterAge = %v, want 3m", got.CollectorGenerationDeadLetters.OldestDeadLetterAge)
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

	if len(queryer.queries) != 29 {
		t.Fatalf("QueryContext() call count = %d, want 29", len(queryer.queries))
	}
	for _, want := range []string{
		"FROM ingestion_scopes",
		"FROM scope_generations",
		"latest_generation_age_seconds",
		"JOIN ingestion_scopes",
		"activated_at",
		"superseded_at",
		"FROM fact_work_items",
		"inflight.conflict_domain",
		"failure_details",
		"SPLIT_PART(fairness_key, ':', 4)",
		"FROM aws_scan_status",
		"FROM aws_freshness_triggers",
		"recent_failed_runs",
		"workflow_collector_backpressure",
		"last_failure_class",
		"WITH active_scopes AS (",
		"JOIN collector_evidence_summary AS summary",
		"workflow_instances AS (",
		"FROM collector_generation_dead_letters",
		"FROM semantic_extraction_jobs",
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
	if !strings.Contains(domainBacklogQuery, "shared_projection_domains AS") {
		t.Fatalf("domainBacklogQuery missing lease-only shared projection domain source:\n%s", domainBacklogQuery)
	}
	if !strings.Contains(domainBacklogQuery, "COALESCE(MAX(active.in_flight_count), 0) > 0") {
		t.Fatalf("domainBacklogQuery missing in-flight shared projection backlog HAVING:\n%s", domainBacklogQuery)
	}
	if !strings.Contains(domainBacklogQuery, "SUM(in_flight_count)") {
		t.Fatalf("domainBacklogQuery final HAVING must include in-flight backlog:\n%s", domainBacklogQuery)
	}
	for name, query := range map[string]string{
		"domainBacklogQuery": domainBacklogQuery,
		"queueSnapshotQuery": queueSnapshotQuery,
	} {
		if !strings.Contains(query, "GREATEST(") {
			t.Fatalf("%s must clamp future timestamps to zero age:\n%s", name, query)
		}
	}
}

// TestDomainBacklogQueryBoundsWorkItemsToNonTerminalStatuses guards issue
// #3389. The fact_domain_backlogs aggregate reports only outstanding,
// in-flight, retrying, dead-letter, and failed depth; it has no succeeded
// field and its HAVING clause discards any domain with zero non-terminal work.
// Once the cloud/SaaS collectors inflated fact_work_items, grouping the entire
// (mostly succeeded) active_fact_work_items population by domain regressed
// status/index past the client timeout. The query MUST filter the
// active_fact_work_items source to the non-terminal status set before the
// GROUP BY so the aggregate uses the (stage, domain, status, ...) index instead
// of scanning every succeeded row. Succeeded and superseded rows contribute 0
// to every FILTER and to the HAVING, so the bound is output-identical.
func TestDomainBacklogQueryBoundsWorkItemsToNonTerminalStatuses(t *testing.T) {
	t.Parallel()

	const boundedSource = `  FROM active_fact_work_items
  WHERE status IN ('pending', 'claimed', 'running', 'retrying', 'dead_letter', 'failed')
  GROUP BY domain`
	if !strings.Contains(domainBacklogQuery, boundedSource) {
		t.Fatalf("domainBacklogQuery must bound fact_domain_backlogs to non-terminal statuses before GROUP BY domain so it does not group every succeeded work item:\n%s", domainBacklogQuery)
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
		if query == awsFreshnessStatusCountsQuery {
			return &fakeRows{}, nil
		}
		if query == awsFreshnessOldestQueuedAgeQuery {
			return &fakeRows{rows: [][]any{{float64(0)}}}, nil
		}
		if query == vulnerabilitySourceStatusQuery {
			return &fakeRows{}, nil
		}
		if query == collectorFactEvidenceQuery {
			return &fakeRows{}, nil
		}
		if query == collectorGenerationDeadLetterStatusQuery {
			return &fakeRows{rows: [][]any{{int64(0), int64(0), int64(0), 0.0}}}, nil
		}
		if query == producerActivityQuery {
			return &fakeRows{rows: [][]any{{false, nil}}}, nil
		}
		if query == semanticExtractionObservabilityQuery {
			return &fakeRows{}, nil
		}
		if query == semanticQueueDepthQuery || query == semanticQueueOldestAgeQuery {
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
		case *sql.NullFloat64:
			switch value := row[i].(type) {
			case nil:
				*target = sql.NullFloat64{}
			case float64:
				*target = sql.NullFloat64{Float64: value, Valid: true}
			default:
				return fmt.Errorf("row[%d] type = %T, want float64 or nil", i, row[i])
			}
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
		case *pq.StringArray:
			switch value := row[i].(type) {
			case nil:
				*target = pq.StringArray{}
			case []string:
				*target = pq.StringArray(value)
			case pq.StringArray:
				*target = value
			default:
				return fmt.Errorf("row[%d] type = %T, want string array", i, row[i])
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
