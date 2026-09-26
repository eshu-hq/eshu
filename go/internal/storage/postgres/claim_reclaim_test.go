// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"regexp"
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
		database:      db,
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
		"generation.status IN ('pending', 'failed')",
		"FROM supersedable_projector_generations AS supersedable",
		"supersedable.work_item_id = work.work_item_id",
		// #7108: every maintenance branch locks first without waiting.
		"JOIN locked_stale_scope_generations AS locked_generation",
		"FROM locked_stale_projector_generations AS locked",
		"FROM locked_stale_projector_duplicates AS locked",
		"FROM locked_claim_siblings AS locked",
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
		"FOR UPDATE OF work SKIP LOCKED",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing %q:\n%s", want, query)
		}
	}
	// Count occurrences: a single substring hit must not satisfy all four lock
	// CTEs (#7108). Three lock CTEs lock rows "OF stale" (duplicates,
	// work rows, siblings) and one locks the generation row.
	for lock, min := range map[string]int{
		"FOR NO KEY UPDATE OF stale SKIP LOCKED":            3,
		"FOR NO KEY UPDATE OF stale_generation SKIP LOCKED": 1,
	} {
		if got := strings.Count(query, lock); got < min {
			t.Fatalf("claim query has %d x %q, want at least %d", got, lock, min)
		}
	}
}

// TestProjectorQueueClaimFencesTheScope pins the #7115 scope claim fence. The
// candidate pool must stay materialized, so its snapshot_fence is the value
// the statement snapshot saw and EvalPlanQual cannot refresh it; without
// MATERIALIZED the planner inlines the pool into one join tree and the fence
// probe is no longer a pkey lookup per pool row. The lock step must lock the
// work row and then the scope's projector_scope_claim_fences row, both SKIP
// LOCKED, and join on fence equality so a scope another claimer bumped after
// the snapshot drops out. LockRows takes row locks in locking-clause order,
// so FOR UPDATE OF work must come first: a busy work row then leaves the
// fence row unlocked (TestProjectorClaimLeavesFenceUnlockedWhenWorkRowBusy
// kills the swapped order). The lock step reads the pool through a subquery
// sorted on the claim-order keys, so the planner nested-loops the probes in
// order and stops at the first lockable row; measured at 2k and 20k scopes,
// it probes one pool row instead of all of them. The claim must bump the
// fence it locked.
func TestProjectorQueueClaimFencesTheScope(t *testing.T) {
	t.Parallel()

	query := claimProjectorWorkQuery
	for _, want := range []string{
		"candidate_pool AS MATERIALIZED (",
		"JOIN projector_scope_claim_fences AS scoped_fence",
		"scoped_fence.fence AS snapshot_fence",
		"FROM (\n        SELECT *\n        FROM candidate_pool\n        ORDER BY\n          reclaim_rank,\n          projector_source_inflight_count ASC,\n          projector_source_fair_rank ASC,\n          updated_at ASC,\n          work_item_id ASC\n    ) AS pool",
		"JOIN projector_scope_claim_fences AS claim_fence",
		"claim_fence.scope_id = pool.scope_id",
		"AND claim_fence.fence = pool.snapshot_fence",
		"FOR UPDATE OF work SKIP LOCKED\n    FOR NO KEY UPDATE OF claim_fence SKIP LOCKED",
		"claimed_scope_fence AS (",
		"UPDATE projector_scope_claim_fences AS fenced_scope",
		"SET fence = fenced_scope.fence + 1",
		"fenced_scope.scope_id = claimed.scope_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing %q:\n%s", want, query)
		}
	}
	// The lock step repeats the row-self predicates on the locked work row so
	// its EvalPlanQual recheck drops a row claimed after the snapshot (#7108).
	lockStep := query[strings.Index(query, "candidate AS ("):strings.Index(query, "claimed AS (")]
	for _, want := range []string{
		"work.status IN ('pending', 'retrying', 'claimed', 'running')",
		"(work.visible_at IS NULL OR work.visible_at <= $1)",
		"(work.claim_until IS NULL OR work.claim_until <= $1)",
	} {
		if !strings.Contains(lockStep, want) {
			t.Fatalf("claim lock step missing row-self predicate %q:\n%s", want, lockStep)
		}
	}
	for _, order := range [][2]string{
		{"candidate_pool AS MATERIALIZED (", "candidate AS ("},
		{"claimed AS (", "claimed_scope_fence AS ("},
	} {
		if strings.Index(query, order[0]) > strings.Index(query, order[1]) {
			t.Fatalf("claim query has %q after %q", order[0], order[1])
		}
	}
}

// TestProjectorQueueClaimNeverLocksIngestionScopes is the #7115 anti-regression
// guard. A row lock on ingestion_scopes can wait even under SKIP LOCKED,
// because FK child inserts KEY SHARE that row and LockRows then walks its
// update chain with a blocking wait; that deadlocked the claim against Ack.
// Every locking clause must name its relation, and none may name an
// ingestion_scopes alias.
func TestProjectorQueueClaimNeverLocksIngestionScopes(t *testing.T) {
	t.Parallel()

	query := claimProjectorWorkQuery
	for _, forbidden := range []string{"projector_claim_fence", "ingestion_scopes AS claim_scope"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("claim query contains forbidden %q:\n%s", forbidden, query)
		}
	}
	// Scan SQL only: comments may quote locking clauses.
	query = regexp.MustCompile(`--[^\n]*`).ReplaceAllString(query, "")
	scopeAliases := map[string]bool{"ingestion_scopes": true}
	for _, match := range regexp.MustCompile(`ingestion_scopes\s+AS\s+(\w+)`).FindAllStringSubmatch(query, -1) {
		scopeAliases[match[1]] = true
	}
	lockClause := regexp.MustCompile(`FOR\s+(UPDATE|NO\s+KEY\s+UPDATE|SHARE|KEY\s+SHARE)\b(\s+OF\s+(\w+))?`)
	clauses := lockClause.FindAllStringSubmatch(query, -1)
	if len(clauses) == 0 {
		t.Fatal("claim query has no locking clauses; the guard would be vacuous")
	}
	for _, clause := range clauses {
		if clause[3] == "" {
			t.Fatalf("locking clause %q names no relation, so it would lock every joined table", clause[0])
		}
		if scopeAliases[clause[3]] {
			t.Fatalf("locking clause %q locks ingestion_scopes (aliases %v)", clause[0], scopeAliases)
		}
	}
}

func TestProjectorQueueClaimScopesBySourceSystem(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 18, 23, 0, 0, 0, time.UTC)
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
					time.Date(2026, time.May, 18, 22, 0, 0, 0, time.UTC),
					time.Date(2026, time.May, 18, 22, 5, 0, 0, time.UTC),
					"pending",
					"snapshot",
					"",
					[]byte(`{"repo_id":"repository:r_test"}`),
				}},
			},
		},
	}

	queue := NewProjectorQueue(db, "bootstrap-index", time.Minute).
		WithClaimSourceSystem("git")
	queue.Now = func() time.Time { return now }

	if _, ok, err := queue.Claim(context.Background()); err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	} else if !ok {
		t.Fatal("Claim() ok = false, want true")
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"$4 = ''",
		"JOIN ingestion_scopes AS candidate_scope",
		"source_system = $4",
		"candidate_scope.scope_id = work.scope_id",
		"same.scope_id = work.scope_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing source-system filter %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[3], "git"; got != want {
		t.Fatalf("source_system arg = %q, want %q", got, want)
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
					int64(0),
					now,
					now,
					now,
					[]byte(`{"entity_key":"repo-123","reason":"shared follow-up","fact_id":"fact-1","source_system":"git"}`),
				}},
			},
		},
	}

	queue := ReducerQueue{
		database:      db,
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
		// The conflict fence intentionally has no live-only (claim_until > $1)
		// scope: a pending sibling defers to ANY claimed/running holder so an
		// expired holder is reclaimed rather than raced (#4137). Proven by
		// TestReducerClaimReclaimsExpiredHolderBeforeOlderPendingSibling.
		"inflight.status IN ('claimed', 'running')",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing %q:\n%s", want, query)
		}
	}
}
