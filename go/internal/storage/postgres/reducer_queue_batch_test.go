// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		// The pre-rewrite query selected the one representative per conflict key
		// with a correlated per-candidate-row subquery
		// (work_item_id = (SELECT same.work_item_id ... ORDER BY ... LIMIT 1)),
		// which re-scanned and re-gated the candidate set per row — the O(N^2)
		// cost the #3624 rank-once rewrite eliminates. The representative is now
		// the reps.same_rn = 1 row: a single row_number() window
		// (w_same, PARTITION BY conflict_domain, ckey) ranked in the #4137
		// claimed/running-first order, computed once for the whole batch. This
		// preserves the identical one-representative-per-conflict-key fence
		// (the expired-holder-first #4137 order and the single-rep-per-key
		// guarantee) without any correlated per-row subquery.
		"reps.same_rn = 1",
		"row_number() OVER w_same",
		"w_same     AS (PARTITION BY conflict_domain, ckey",
		"ORDER BY claimed_running_first ASC, is_search_doc ASC, updated_at ASC, work_item_id ASC),",
		// The rank-once rewrite (#3624) moves FOR UPDATE SKIP LOCKED to an
		// outer `locked` CTE that joins back to `candidate` by primary key
		// (window functions are forbidden at the same query level as FOR
		// UPDATE SKIP LOCKED, but are legal in the inner CTEs this level
		// joins against). "FOR UPDATE OF lock_target SKIP LOCKED" is the
		// same lease-safe skip-locked claim property, expressed with an
		// explicit lock target instead of an implicit single-relation lock.
		"FOR UPDATE OF lock_target SKIP LOCKED",
		// Because the lock now sits on lock_target (joined to the snapshot
		// candidate set by id) rather than directly on the predicate-bearing
		// candidate SELECT, the row-self lease/visibility/status predicates MUST
		// be re-applied on lock_target so PostgreSQL's Read Committed
		// EvalPlanQual recheck drops a row another worker claimed and committed
		// between our snapshot and this lock (otherwise its fresh lease is
		// overwritten — lease theft). See TestClaimBatchLockRecheckDropsConcurrentlyClaimedRow.
		"WHERE lock_target.stage = 'reducer'",
		"AND lock_target.status IN ('pending', 'retrying', 'claimed', 'running')",
		"AND (lock_target.claim_until IS NULL OR lock_target.claim_until <= $1)",
		"AND (lock_target.visible_at IS NULL OR lock_target.visible_at <= $1)",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing %q:\n%s", want, query)
		}
	}
}

func TestClaimBatchOrdersSearchDocumentCatchupAfterGraphTruth(t *testing.T) {
	t.Parallel()

	query := claimReducerWorkBatchQuery
	// The rank-once rewrite (#3624) computes the search-document deprioritization
	// flag exactly once in the base CTE (is_search_doc) instead of re-deriving a
	// `CASE WHEN same.domain = ...` at a correlated same-representative call site.
	searchRank := "CASE WHEN fact_work_items.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS is_search_doc"
	if !strings.Contains(query, searchRank) {
		t.Fatalf("batch claim query missing search-document deprioritization rank:\n%s", query)
	}
	// The conflict-key representative (same_rn = 1) must deprioritize search
	// documents: the w_same window orders by is_search_doc ASC, so a
	// non-search-document sibling outranks a search document on the same
	// conflict key and becomes the representative. This is the rank-once
	// equivalent of the removed `CASE WHEN same.domain = ...` correlated rank.
	for _, want := range []string{
		"w_same     AS (PARTITION BY conflict_domain, ckey",
		"ORDER BY claimed_running_first ASC, is_search_doc ASC, updated_at ASC, work_item_id ASC),",
		"row_number() OVER w_same     AS same_rn",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("same-conflict representative window missing search-document deprioritization order %q:\n%s", want, query)
		}
	}
	if !strings.Contains(query, "ORDER BY reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, updated_at ASC, work_item_id ASC") {
		t.Fatalf("batch claim query missing domain-priority and fairness ordering:\n%s", query)
	}
	// The per-domain fairness rank (#3385) must round-robin ready domains so a
	// high-volume older backlog cannot starve a newer, lower-volume domain.
	if !strings.Contains(query, "AS reducer_domain_fair_rank") {
		t.Fatalf("batch claim query missing per-domain fairness rank:\n%s", query)
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
		// Expired claimed/running holders enter the base candidate set...
		"status IN ('pending', 'retrying', 'claimed', 'running')",
		// ...only when their lease is unset or already expired (the reclaim gate)...
		"claim_until IS NULL OR claim_until <= $1",
		// ...and the #4137 rank-once representative order keeps the expired holder
		// the conflict-key representative (same_rn = 1) ahead of any pending sibling.
		// This replaces the pre-rewrite correlated same.status IN (...) subquery.
		"CASE WHEN fact_work_items.status IN ('claimed', 'running') THEN 0 ELSE 1 END AS claimed_running_first",
		"ORDER BY claimed_running_first ASC, is_search_doc ASC, updated_at ASC, work_item_id ASC)",
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
		SemanticEntityClaimLimit:         1,
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
		SemanticEntityClaimLimit:         1,
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

func TestClaimBatchGatesAWSRelationshipsOnCanonicalCloudResourceReadiness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 10, 5, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	intents, err := queue.ClaimBatch(context.Background(), 5)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("ClaimBatch() returned %d intents from empty db, want 0", len(intents))
	}

	query := db.queries[0].query
	if !queryHasBoundedReadinessRequirement(
		query,
		string(reducer.DomainAWSRelationshipMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("batch claim query missing AWS relationship readiness requirement:\n%s", query)
	}
	if !queryHasPayloadReadinessLookup(query, "fact_work_items", "readiness_req", "readiness_phase") {
		t.Fatalf("batch claim query missing AWS relationship payload readiness lookup:\n%s", query)
	}
	if !queryHasRankOnceRepresentativeReadinessGate(query, "fact_work_items", "readiness_req", "readiness_phase") {
		t.Fatalf("batch claim query missing AWS relationship representative readiness lookup:\n%s", query)
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
	got, ok := db.queries[0].args[1].([]string)
	if !ok || len(got) != 1 || got[0] != string(reducer.DomainSQLRelationshipMaterialization) {
		t.Fatalf("domain filter arg = %#v, want [%q]", db.queries[0].args[1], reducer.DomainSQLRelationshipMaterialization)
	}
}

func TestReducerQueueImplementsBatchInterfaces(t *testing.T) {
	t.Parallel()

	q := NewReducerQueue(&fakeExecQueryer{}, "test", time.Minute)

	// Compile-time check that ReducerQueue implements both batch interfaces.
	var _ reducer.BatchWorkSource = q
	var _ reducer.BatchWorkSink = q
}
