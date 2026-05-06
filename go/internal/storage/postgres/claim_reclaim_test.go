package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProjectorQueueClaimIncludesExpiredLeaseReclaimPredicates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"scope-123",
					"git",
					"repository",
					"",
					"",
					false,
					"git",
					"repo-123",
					"generation-456",
					1,
					time.Date(2026, time.April, 12, 10, 0, 0, 0, time.UTC),
					time.Date(2026, time.April, 12, 10, 5, 0, 0, time.UTC),
					"pending",
					"snapshot",
					"",
					[]byte(`{"repo_id":"repository:r_test"}`),
				}},
			},
		},
	}

	queue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: 30 * time.Second,
		Now:           func() time.Time { return now },
	}

	if _, ok, err := queue.Claim(context.Background()); err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	} else if !ok {
		t.Fatal("Claim() ok = false, want true")
	}

	query := db.queries[0].query
	for _, want := range []string{
		"status IN ('pending', 'retrying', 'claimed', 'running')",
		"work.claim_until IS NULL OR work.claim_until <= $1",
		"work.visible_at IS NULL OR work.visible_at <= $1",
		"NOT EXISTS (",
		"inflight.scope_id = work.scope_id",
		"inflight.status IN ('claimed', 'running')",
		"inflight.claim_until > $1",
		"reclaimed_stale_projector_duplicates AS (",
		"status = 'retrying'",
		"live.scope_id = stale.scope_id",
		"live.claim_until > $1",
		"superseded_stale_projector_generations AS (",
		"status = 'superseded'",
		"projector_superseded_by_newer_generation",
		"stale_generation.generation_id = stale.generation_id",
		"newer_generation.generation_id = newer.generation_id",
		"newer.scope_id = stale.scope_id",
		"newer.status IN ('pending', 'retrying', 'claimed', 'running', 'succeeded', 'failed', 'dead_letter', 'superseded')",
		"newer_generation.ingested_at > stale_generation.ingested_at",
		"RETURNING stale.work_item_id, stale.generation_id",
		"superseded_stale_scope_generations AS (",
		"FROM superseded_stale_projector_generations AS stale",
		"generation.status = 'pending'",
		"FROM superseded_stale_projector_generations AS superseded",
		"superseded.work_item_id = work.work_item_id",
		"FROM superseded_stale_projector_generations AS superseded_same",
		"superseded_same.work_item_id = same.work_item_id",
		"reclaimed_claim_siblings AS (",
		"FROM claimed",
		"stale.scope_id = claimed.scope_id",
		"stale.work_item_id <> claimed.work_item_id",
		"projector_stale_scope_reclaim",
		"work.work_item_id = (",
		"same.status IN ('claimed', 'running') AND same.claim_until <= $1 THEN 0",
		"same.stage = 'projector'",
		"same.scope_id = work.scope_id",
		"same.status IN ('pending', 'retrying', 'claimed', 'running')",
		"same.work_item_id ASC",
		"work.status IN ('claimed', 'running') AND work.claim_until <= $1 THEN 0",
		"prior_generation.generation_id <> claimed.generation_id",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing %q:\n%s", want, query)
		}
	}
}

func TestReducerQueueClaimIncludesExpiredLeaseReclaimPredicates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"reducer_scope-123_generation-456_workload_identity_repo-123_fact-1_20260412110000.000000000_0",
					"scope-123",
					"generation-456",
					"workload_identity",
					1,
					now,
					now,
					[]byte(`{"entity_key":"repo-123","reason":"shared follow-up","fact_id":"fact-1","source_system":"git"}`),
				}},
			},
		},
	}

	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	if _, ok, err := queue.Claim(context.Background()); err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	} else if !ok {
		t.Fatal("Claim() ok = false, want true")
	}

	query := db.queries[0].query
	for _, want := range []string{
		"status IN ('pending', 'retrying', 'claimed', 'running')",
		"claim_until IS NULL OR claim_until <= $1",
		"visible_at IS NULL OR visible_at <= $1",
		"NOT EXISTS (",
		"inflight.conflict_domain = fact_work_items.conflict_domain",
		"COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)",
		"inflight.work_item_id <> fact_work_items.work_item_id",
		"inflight.status IN ('claimed', 'running')",
		"inflight.claim_until > $1",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing %q:\n%s", want, query)
		}
	}
}
