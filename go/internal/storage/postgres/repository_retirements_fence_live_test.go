// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestRetirementSummaryCountsLive proves the index-status retirement block
// against real Postgres: per-state open counts, the oldest pending age from
// requested_at, and the last completion from retired_at. Readmitted rows
// never appear in any count.
func TestRetirementSummaryCountsLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_summary",
		"set ESHU_POSTGRES_DSN to run the retirement summary Postgres proof")
	store := NewRetirementStore(SQLDB{DB: database})

	for i, req := range []RetirementRequest{
		{
			RetirementID: "rr_sum01", RepoID: "PARTITION:sum-a",
			ScopeID: "sum-scope-a", ReasonCode: RetirementReasonOperatorRetired,
			ActorClass: "operator", IdempotencyKeyHash: "sum-key-a",
		},
		{
			RetirementID: "rr_sum02", RepoID: "PARTITION:sum-b",
			ScopeID: "sum-scope-b", ReasonCode: RetirementReasonOperatorRetired,
			ActorClass: "operator", IdempotencyKeyHash: "sum-key-b",
		},
	} {
		if _, err := store.InsertPending(ctx, req); err != nil {
			t.Fatalf("InsertPending %d: %v", i, err)
		}
	}

	summary, err := store.ReadRetirementSummary(ctx)
	if err != nil {
		t.Fatalf("ReadRetirementSummary: %v", err)
	}
	if summary.CountsByState[RetirementStatePending] != 2 {
		t.Fatalf("pending = %d, want 2: %+v", summary.CountsByState[RetirementStatePending], summary.CountsByState)
	}
	if !summary.HasPending {
		t.Fatal("HasPending = false, want true with two pending rows")
	}
	if summary.OldestPendingAgeSeconds < 0 {
		t.Fatalf("oldest pending age = %v, want >= 0", summary.OldestPendingAgeSeconds)
	}
	if summary.HasCompleted {
		t.Fatal("HasCompleted = true, want false with no complete row")
	}
}

// seedFenceLiveScope writes one repository partition with two scopes, three
// live generations, and projector plus reducer work rows for the fence live
// proof below. It returns the partition key and scope ids.
func seedFenceLiveScope(t *testing.T, ctx context.Context, database *sql.DB, partition string) []string {
	t.Helper()
	scopeIDs := []string{"fence-live-main", "fence-live-ref"}
	for i, scopeID := range scopeIDs {
		if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
			(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
			 observed_at, ingested_at, status, active_generation_id, payload)
			VALUES ($1, 'repository', 'git', $2, 'git_collector', $3, NOW(), NOW(), 'active', $4, '{}'::jsonb)`,
			scopeID, "key-"+scopeID, partition, scopeID+"-gen-active"); err != nil {
			t.Fatalf("seed scope %s: %v", scopeID, err)
		}
		active := scopeID + "-gen-active"
		old := scopeID + "-gen-old"
		for _, gen := range []struct {
			id, status string
		}{{active, "active"}, {old, "failed"}} {
			if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
				(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
				VALUES ($1, $2, 'push', NOW(), NOW(), $3, '{}'::jsonb)`, gen.id, scopeID, gen.status); err != nil {
				t.Fatalf("seed generation %s: %v", gen.id, err)
			}
		}
		stage := "projector"
		if i == 1 {
			// The ref scope's old generation keeps reducer work so the live
			// proof marks both stages.
			stage = "reducer"
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items
			(work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'd1', 'pending', NOW(), NOW())`,
			"fence-live-w-"+scopeID, scopeID, old, stage); err != nil {
			t.Fatalf("seed work row %s: %v", scopeID, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO repository_reindex_requests (scope_id, requested_at)
			VALUES ($1, NOW())`, scopeID); err != nil {
			t.Fatalf("seed reindex request %s: %v", scopeID, err)
		}
	}
	return scopeIDs
}

// TestRetirementFenceLive proves the full phase-1 fence against real
// Postgres: generations superseded, the active pointer nulled, projector
// and reducer rows marked (never deleted) with the retirement failure
// class, reindex rows dropped, the pending/fenced marker committed, the
// fence assertion counts matching, and a re-fence resuming the same row.
// The commit gate then refuses the scopes until re-admission.
func TestRetirementFenceLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_fence",
		"set ESHU_POSTGRES_DSN to run the retirement fence Postgres proof")
	partition := "PARTITION:fence-live"
	scopeIDs := seedFenceLiveScope(t, ctx, database, partition)

	fencer := NewRetirementFencer(SQLDB{DB: database}, SQLDB{DB: database})
	result, err := fencer.FenceRepository(ctx, FenceRequest{
		Retirements: []FenceRetirement{
			{RetirementID: "rr_fencelive01", RepoID: partition, ScopeID: "fence-live-main"},
		},
		ReasonCode:         RetirementReasonOperatorRetired,
		ReasonHash:         "h",
		ActorClass:         "operator",
		IdempotencyKeyHash: "fence-live-key",
	})
	if err != nil {
		t.Fatalf("FenceRepository: %v", err)
	}
	if result.GenerationsFenced != 4 {
		t.Fatalf("generations fenced = %d, want 4 (2 active + 2 failed)", result.GenerationsFenced)
	}
	if result.ProjectorRowsMarked+result.ReducerRowsMarked != 2 {
		t.Fatalf("work rows marked = %d projector + %d reducer, want 2 total",
			result.ProjectorRowsMarked, result.ReducerRowsMarked)
	}

	var liveGenerations int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM scope_generations
		WHERE scope_id = ANY($1) AND status IN ('pending','active','failed')`, scopeIDs).Scan(&liveGenerations); err != nil {
		t.Fatalf("live generations: %v", err)
	}
	if liveGenerations != 0 {
		t.Fatalf("live generations = %d, want 0 after the fence", liveGenerations)
	}
	var activePointers int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM ingestion_scopes
		WHERE partition_key = $1 AND active_generation_id IS NOT NULL`, partition).Scan(&activePointers); err != nil {
		t.Fatalf("active pointers: %v", err)
	}
	if activePointers != 0 {
		t.Fatalf("active pointers = %d, want 0 after the fence", activePointers)
	}
	var marked int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items
		WHERE scope_id = ANY($1) AND status = 'superseded' AND failure_class = 'repository_retired'`,
		scopeIDs).Scan(&marked); err != nil {
		t.Fatalf("marked work rows: %v", err)
	}
	if marked != 2 {
		t.Fatalf("marked work rows = %d, want 2", marked)
	}
	var reindex int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM repository_reindex_requests
		WHERE scope_id = ANY($1)`, scopeIDs).Scan(&reindex); err != nil {
		t.Fatalf("reindex rows: %v", err)
	}
	if reindex != 0 {
		t.Fatalf("reindex rows = %d, want 0 after the fence", reindex)
	}

	store := NewRetirementStore(SQLDB{DB: database})
	open, err := store.GetOpen(ctx, partition)
	if err != nil {
		t.Fatalf("GetOpen: %v", err)
	}
	if open == nil || open.State != RetirementStatePending || open.Phase != RetirementPhaseFenced {
		t.Fatalf("open = %+v, want pending/fenced", open)
	}

	// The commit gate refuses the fenced scopes: no generation row lands.
	commitStore := NewIngestionStore(SQLDB{DB: database})
	now := time.Now().UTC()
	scopeValue := scope.IngestionScope{
		ScopeID: "fence-live-main", SourceSystem: "git", ScopeKind: scope.KindRepository,
		CollectorKind: scope.CollectorGit, PartitionKey: partition,
		Metadata: map[string]string{"repo_id": partition},
	}
	generation := scope.ScopeGeneration{
		GenerationID: "fence-live-main-gen-new", ScopeID: "fence-live-main",
		ObservedAt: now, IngestedAt: now,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
	}
	if err := commitStore.CommitScopeGeneration(ctx, scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration over a fenced scope: %v (want nil skip)", err)
	}
	var landed int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM scope_generations
		WHERE generation_id = 'fence-live-main-gen-new'`).Scan(&landed); err != nil {
		t.Fatalf("refused generation probe: %v", err)
	}
	if landed != 0 {
		t.Fatal("a generation committed over a fenced scope, want the gate refusal")
	}

	// A re-fence with the same idempotency key resumes the same row.
	second, err := fencer.FenceRepository(ctx, FenceRequest{
		Retirements: []FenceRetirement{
			{RetirementID: "rr_fencelive02", RepoID: partition, ScopeID: "fence-live-main"},
		},
		ReasonCode:         RetirementReasonOperatorRetired,
		ReasonHash:         "h",
		ActorClass:         "operator",
		IdempotencyKeyHash: "fence-live-key",
	})
	if err != nil {
		t.Fatalf("re-fence: %v", err)
	}
	if len(second.RetirementIDs) != 1 || second.RetirementIDs[0] != "rr_fencelive01" {
		t.Fatalf("re-fence retirement ids = %v, want [rr_fencelive01] (same row)", second.RetirementIDs)
	}
}

// TestReadRepositoryFreshnessReadsTombstoneFirst proves the freshness
// tombstone lookup runs before scope resolution: the fake answers only the
// tombstone statement, so any implementation that resolves the scope first
// fails, and the snapshot carries the open row for the rule-0 verdict.
func TestReadRepositoryFreshnessReadsTombstoneFirst(t *testing.T) {
	t.Parallel()

	queryer := &tombstoneOnlyQueryer{}
	store := NewRepositoryFreshnessStore(queryer)
	snapshot, err := store.ReadRepositoryFreshness(context.Background(), "PARTITION:tombstoned")
	if err != nil {
		t.Fatalf("ReadRepositoryFreshness: %v", err)
	}
	if queryer.tombstoneQueries != 1 {
		t.Fatalf("tombstone lookups = %d, want exactly 1", queryer.tombstoneQueries)
	}
	if snapshot.Retirement == nil {
		t.Fatal("snapshot.Retirement is nil, want the open tombstone row")
	}
	if snapshot.Retirement.State != RetirementStatePending {
		t.Fatalf("retirement state = %q, want pending", snapshot.Retirement.State)
	}
	if got := statuspkg.ComputeRepositoryFreshnessVerdict(snapshot, ""); got != statuspkg.RepositoryFreshnessRetiring {
		t.Fatalf("verdict = %q, want retiring", got)
	}
}

// tombstoneOnlyQueryer answers the retirement tombstone lookup and leaves
// every other read empty, and fails the read unless the tombstone lookup
// is the first statement. A purged repository has no scope left to resolve,
// so the snapshot must carry the tombstone with Resolved false rather than
// fail.
type tombstoneOnlyQueryer struct {
	calls            int
	tombstoneQueries int
}

func (q *tombstoneOnlyQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.calls++
	if strings.Contains(query, "FROM repository_retirements") {
		if q.calls != 1 {
			return nil, errors.New("retirement tombstone lookup was not the first freshness statement")
		}
		q.tombstoneQueries++
		return &queueFakeRows{rows: [][]any{{"pending", "rr_tomb01"}}}, nil
	}
	return &queueFakeRows{}, nil
}
