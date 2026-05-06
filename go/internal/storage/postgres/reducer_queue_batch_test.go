package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestClaimBatchValidationRejectsEmptyQueue(t *testing.T) {
	t.Parallel()

	q := ReducerQueue{}
	_, err := q.ClaimBatch(context.Background(), 5)
	if err == nil {
		t.Fatal("ClaimBatch() with zero-value queue should fail validation")
	}
}

func TestAckBatchValidationRejectsEmptyQueue(t *testing.T) {
	t.Parallel()

	q := ReducerQueue{}
	err := q.AckBatch(context.Background(), []reducer.Intent{{IntentID: "test"}}, nil)
	if err == nil {
		t.Fatal("AckBatch() with zero-value queue should fail validation")
	}
}

func TestAckBatchEmptyIsNoop(t *testing.T) {
	t.Parallel()

	q := ReducerQueue{
		db:            &fakeExecQueryer{},
		LeaseOwner:    "test",
		LeaseDuration: time.Minute,
	}

	err := q.AckBatch(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("AckBatch(nil) error = %v, want nil", err)
	}
}

func TestClaimBatchReturnsEmptyFromEmptyDB(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil}, // empty result set
		},
	}
	q := ReducerQueue{
		db:            db,
		LeaseOwner:    "test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC) },
	}

	intents, err := q.ClaimBatch(context.Background(), 5)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("ClaimBatch() returned %d intents from empty db, want 0", len(intents))
	}
}

func TestClaimBatchReturnsClaimedIntents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"item-1", "scope-1", "gen-1", "code_call_materialization", 1, now, now, []byte(`{"entity_key":"key-1","reason":"test","fact_id":"f1","source_system":"git"}`)},
				{"item-2", "scope-2", "gen-2", "code_call_materialization", 1, now, now, []byte(`{"entity_key":"key-2","reason":"test","fact_id":"f2","source_system":"git"}`)},
			}},
		},
	}
	q := ReducerQueue{
		db:            db,
		LeaseOwner:    "test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	intents, err := q.ClaimBatch(context.Background(), 5)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if got, want := len(intents), 2; got != want {
		t.Fatalf("ClaimBatch() returned %d intents, want %d", got, want)
	}
	if intents[0].IntentID != "item-1" {
		t.Fatalf("intents[0].IntentID = %q, want %q", intents[0].IntentID, "item-1")
	}
	if intents[1].IntentID != "item-2" {
		t.Fatalf("intents[1].IntentID = %q, want %q", intents[1].IntentID, "item-2")
	}
}

func TestClaimBatchFencesSameConflictCandidates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	q := ReducerQueue{
		db:            db,
		LeaseOwner:    "test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	if _, err := q.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	query := db.queries[0].query
	for _, want := range []string{
		"NOT EXISTS (",
		"inflight.conflict_domain = fact_work_items.conflict_domain",
		"COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)",
		"inflight.work_item_id <> fact_work_items.work_item_id",
		"inflight.status IN ('claimed', 'running')",
		"inflight.claim_until > $1",
		"work_item_id = (",
		"same.conflict_domain = fact_work_items.conflict_domain",
		"COALESCE(same.conflict_key, same.scope_id) = COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)",
		"same.status IN ('pending', 'retrying', 'claimed', 'running')",
		"ORDER BY same.updated_at ASC, same.work_item_id ASC",
		"LIMIT 1",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing %q:\n%s", want, query)
		}
	}
}

func TestClaimBatchCanReclaimExpiredClaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	q := ReducerQueue{
		db:            db,
		LeaseOwner:    "test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	if _, err := q.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	query := db.queries[0].query
	for _, want := range []string{
		"status IN ('pending', 'retrying', 'claimed', 'running')",
		"same.status IN ('pending', 'retrying', 'claimed', 'running')",
		"claim_until IS NULL OR claim_until <= $1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing expired-claim reclaim predicate %q:\n%s", want, query)
		}
	}
}

func TestClaimBatchCanWaitForProjectorDrain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	q := ReducerQueue{
		db:                               db,
		LeaseOwner:                       "test",
		LeaseDuration:                    time.Minute,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
	}

	if _, err := q.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	query := db.queries[0].query
	for _, want := range []string{
		"$5 = false OR NOT EXISTS",
		"projector_work.stage = 'projector'",
		"projector_work.scope_id = fact_work_items.scope_id",
		"projector_work.status IN ('pending', 'retrying', 'claimed', 'running')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing projector drain predicate %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[4], true; got != want {
		t.Fatalf("projector drain arg = %v, want %v", got, want)
	}
	if got, want := db.queries[0].args[7], 5; got != want {
		t.Fatalf("limit arg = %v, want %v", got, want)
	}
}

func TestClaimBatchGatesSemanticEntitiesOnGlobalProjectorDrain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	q := ReducerQueue{
		db:                               db,
		LeaseOwner:                       "test",
		LeaseDuration:                    time.Minute,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
	}

	if _, err := q.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	query := db.queries[0].query
	for _, want := range []string{
		"domain <> 'semantic_entity_materialization'",
		"projector_any.stage = 'projector'",
		"projector_any.domain = 'source_local'",
		"projector_any.status IN ('pending', 'retrying', 'claimed', 'running')",
		"projector_done.domain = 'source_local'",
		"projector_done.status = 'succeeded'",
		">= $6",
		"semantic_inflight.domain = 'semantic_entity_materialization'",
		"semantic_inflight.status IN ('claimed', 'running')",
		"semantic_inflight.claim_until > $1",
		"< $7",
		"semantic_next.domain = 'semantic_entity_materialization'",
		"semantic_next.updated_at < fact_work_items.updated_at",
		"semantic_next.work_item_id <= fact_work_items.work_item_id",
		"<= $7 - (",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing semantic global projector gate %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[6], 1; got != want {
		t.Fatalf("semantic claim limit arg = %v, want %v", got, want)
	}
}

func TestClaimBatchPassesExpectedSourceLocalProjectors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 2, 4, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	q := ReducerQueue{
		db:                            db,
		LeaseOwner:                    "test",
		LeaseDuration:                 time.Minute,
		Now:                           func() time.Time { return now },
		ExpectedSourceLocalProjectors: 878,
	}

	if _, err := q.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	if got, want := db.queries[0].args[5], 878; got != want {
		t.Fatalf("expected source-local projector arg = %v, want %v", got, want)
	}
	if got, want := db.queries[0].args[7], 5; got != want {
		t.Fatalf("limit arg = %v, want %v", got, want)
	}
}

func TestClaimBatchPassesSemanticEntityClaimLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 2, 15, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	q := ReducerQueue{
		db:                               db,
		LeaseOwner:                       "test",
		LeaseDuration:                    time.Minute,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
		SemanticEntityClaimLimit:         4,
	}

	if _, err := q.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	if got, want := db.queries[0].args[6], 4; got != want {
		t.Fatalf("semantic claim limit arg = %v, want %v", got, want)
	}
	if got, want := db.queries[0].args[7], 5; got != want {
		t.Fatalf("limit arg = %v, want %v", got, want)
	}
}

func TestClaimBatchCanFilterByDomain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 28, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	q := ReducerQueue{
		db:            db,
		LeaseOwner:    "sql-lane",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomain:   reducer.DomainSQLRelationshipMaterialization,
	}

	if _, err := q.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if got, want := db.queries[0].args[1], string(reducer.DomainSQLRelationshipMaterialization); got != want {
		t.Fatalf("domain filter arg = %v, want %v", got, want)
	}
}

func TestReducerQueueImplementsBatchInterfaces(t *testing.T) {
	t.Parallel()

	q := NewReducerQueue(&fakeExecQueryer{}, "test", time.Minute)

	// Compile-time check that ReducerQueue implements both batch interfaces.
	var _ reducer.BatchWorkSource = q
	var _ reducer.BatchWorkSink = q
}
