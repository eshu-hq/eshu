// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10972 disposable PostgreSQL proof that the unstamped re-drive fix holds behaviorally: a retouch keeps an outstanding row's queue position while advancing its cycle bound, and a fenced workload replay reopens a succeeded row only on a fence change
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// Test10972UnstampedRedriveLive proves the #10972 fix over real PostgreSQL:
// the writer pair behind the Decision-3 unstamped re-drive no longer starves
// the lane on no-change cycles.
func Test10972UnstampedRedriveLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "unstamped-redrive-10972",
		"set ESHU_POSTGRES_DSN to run the #10972 unstamped re-drive proof")

	suffix := testSuffix(t)
	scope := "test-10972-redrive-" + suffix
	gen := scope + "-gen"
	seed10972Scope(t, ctx, db, scope, gen)

	t.Run("retouch keeps outstanding queue position", func(t *testing.T) {
		prove10972RetouchKeepsQueuePosition(t, ctx, db, scope, gen)
	})
	t.Run("fenced replay reopens succeeded only on fence change", func(t *testing.T) {
		prove10972FencedReplayGatesOnFenceChange(t, ctx, db, scope, gen)
	})
	t.Run("fenced replay reopens absent fence once then holds", func(t *testing.T) {
		prove10972FencedReplayReopensAbsentFenceOnce(t, ctx, db, scope, gen)
	})
}

func seed10972Scope(t *testing.T, ctx context.Context, db *sql.DB, scope, gen string) {
	t.Helper()
	for _, seed := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload) VALUES ($1,'vulnerability_intelligence','synthetic',$1,'synthetic',$1,NOW(),NOW(),'active','{}'::jsonb) ON CONFLICT (scope_id) DO NOTHING`, []any{scope}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload) VALUES ($1,$2,'synthetic',NOW(),NOW(),'active',NOW(),'{}'::jsonb) ON CONFLICT (generation_id) DO NOTHING`, []any{gen, scope}},
	} {
		if _, err := db.ExecContext(ctx, seed.q, seed.args...); err != nil {
			t.Fatalf("seed scope/generation: %v", err)
		}
	}
}

type queueRow10972 struct {
	status     string
	updatedAt  time.Time
	reopenedAt sql.NullTime
	attempts   int
	owner      sql.NullString
	fence      string
}

func readQueueRow10972(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) queueRow10972 {
	t.Helper()
	var row queueRow10972
	if err := db.QueryRowContext(ctx, `
SELECT status, updated_at, reopened_at, attempt_count, lease_owner,
       COALESCE(payload->>'repo_dependency_readiness_fence', '')
FROM fact_work_items
WHERE work_item_id = $1`, workItemID).Scan(
		&row.status, &row.updatedAt, &row.reopenedAt, &row.attempts, &row.owner, &row.fence,
	); err != nil {
		t.Fatalf("read work item: %v", err)
	}
	return row
}

// prove10972RetouchKeepsQueuePosition shows a retouch of an already-outstanding
// row preserves updated_at (the fair-rank claim order keeps the row's queue
// position instead of requeueing it behind the backlog) while still advancing
// reopened_at (the winners-watermark bound covers the later touch), and that a
// retouch of a terminal row still reopens a fresh repair cycle.
func prove10972RetouchKeepsQueuePosition(t *testing.T, ctx context.Context, db *sql.DB, scope, gen string) {
	t.Helper()
	queue := NewReducerQueue(SQLDB{DB: db}, "retouch-proof-10972", time.Minute)
	queue.MaxAttempts = 10
	queue.ClaimDomains = []reducercontract.Domain{reducercontract.DomainAdvisoryGroupProjection}
	intent := runtime.ReducerIntent{
		ScopeID:      scope,
		GenerationID: gen,
		Domain:       reducercontract.DomainAdvisoryGroupProjection,
		EntityKey:    "advisory_group:CVE-10972-REDRIVE",
		Reason:       "unstamped re-drive proof",
		SourceSystem: "synthetic",
		Payload:      map[string]any{"group_id": "CVE-10972-REDRIVE"},
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	workItemID := reducerWorkItemID(intent)

	before := readQueueRow10972(t, ctx, db, workItemID)
	if before.status != "pending" {
		t.Fatalf("seeded status = %q, want pending", before.status)
	}
	time.Sleep(20 * time.Millisecond)

	retouchAt := time.Now().UTC()
	if ok, err := queue.RetouchWorkItem(ctx, workItemID); err != nil || !ok {
		t.Fatalf("retouch pending: ok=%v err=%v", ok, err)
	}
	after := readQueueRow10972(t, ctx, db, workItemID)
	if after.status != "pending" {
		t.Fatalf("retouch changed status to %q, want pending kept", after.status)
	}
	if !after.updatedAt.Equal(before.updatedAt) {
		t.Fatalf("retouch moved updated_at from %v to %v on an outstanding row: the row loses its queue position",
			before.updatedAt, after.updatedAt)
	}
	if !after.reopenedAt.Valid || after.reopenedAt.Time.Before(retouchAt.Add(-time.Second)) {
		t.Fatalf("retouch reopened_at = %v, want at or after the touch so the bound covers it", after.reopenedAt)
	}

	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := queue.Ack(ctx, claimed, reducercontract.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	succeeded := readQueueRow10972(t, ctx, db, workItemID)
	if succeeded.status != "succeeded" {
		t.Fatalf("after ACK status = %q, want succeeded", succeeded.status)
	}
	time.Sleep(20 * time.Millisecond)

	if ok, err := queue.RetouchWorkItem(ctx, workItemID); err != nil || !ok {
		t.Fatalf("retouch succeeded: ok=%v err=%v", ok, err)
	}
	reopened := readQueueRow10972(t, ctx, db, workItemID)
	if reopened.status != "pending" {
		t.Fatalf("retouch of a succeeded row left status = %q, want pending for a new repair cycle", reopened.status)
	}
	if !reopened.updatedAt.After(succeeded.updatedAt) {
		t.Fatalf("retouch of a succeeded row kept updated_at = %v, want a fresh repair cycle after %v",
			reopened.updatedAt, succeeded.updatedAt)
	}
	if reopened.attempts != 0 {
		t.Fatalf("reopened attempt_count = %d, want 0", reopened.attempts)
	}
}

// prove10972FencedReplayGatesOnFenceChange shows a fenced workload replay with
// an unchanged fence leaves a succeeded row (and its clocks) alone while still
// reporting scheduled, and that a changed fence reopens it.
func prove10972FencedReplayGatesOnFenceChange(t *testing.T, ctx context.Context, db *sql.DB, scope, gen string) {
	t.Helper()
	queue := NewReducerQueue(SQLDB{DB: db}, "fenced-replay-proof-10972", time.Minute)
	queue.MaxAttempts = 10
	queue.ClaimDomain = reducer.DomainWorkloadMaterialization
	entityKey := "repo:workload-10972-redrive"
	repoID := "repository:r_10972_redrive"
	intent := runtime.ReducerIntent{
		ScopeID:      scope,
		GenerationID: gen,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       "unstamped re-drive proof",
		SourceSystem: "reducer",
		Payload: map[string]any{
			reducer.RepoDependencyReadinessFencePayloadKey:  "fence-a",
			reducer.RepoDependencyReadinessRepoIDPayloadKey: repoID,
		},
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := queue.Ack(ctx, claimed, reducercontract.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	succeeded := readQueueRow10972(t, ctx, db, claimed.IntentID)
	if succeeded.status != "succeeded" || succeeded.fence != "fence-a" {
		t.Fatalf("after ACK status = %q fence = %q, want succeeded with fence-a", succeeded.status, succeeded.fence)
	}

	replayed, err := queue.ReplayWorkloadMaterializationForFence(ctx, scope, gen, entityKey, repoID, "fence-a")
	if err != nil || !replayed {
		t.Fatalf("same-fence replay = (%v, %v), want (true, nil): a no-change cycle must still report scheduled", replayed, err)
	}
	unchanged := readQueueRow10972(t, ctx, db, claimed.IntentID)
	if unchanged.status != "succeeded" {
		t.Fatalf("same-fence replay moved status to %q, want succeeded kept", unchanged.status)
	}
	if !unchanged.updatedAt.Equal(succeeded.updatedAt) {
		t.Fatalf("same-fence replay moved updated_at from %v to %v: a no-change cycle must not requeue the row",
			succeeded.updatedAt, unchanged.updatedAt)
	}
	time.Sleep(20 * time.Millisecond)

	replayed, err = queue.ReplayWorkloadMaterializationForFence(ctx, scope, gen, entityKey, repoID, "fence-b")
	if err != nil || !replayed {
		t.Fatalf("changed-fence replay = (%v, %v), want (true, nil)", replayed, err)
	}
	reopened := readQueueRow10972(t, ctx, db, claimed.IntentID)
	if reopened.status != "pending" {
		t.Fatalf("changed-fence replay left status = %q, want pending", reopened.status)
	}
	if reopened.fence != "fence-b" {
		t.Fatalf("changed-fence replay left fence = %q, want fence-b", reopened.fence)
	}
	if !reopened.updatedAt.After(succeeded.updatedAt) {
		t.Fatalf("changed-fence replay kept updated_at = %v, want a fresh cycle after %v",
			reopened.updatedAt, succeeded.updatedAt)
	}
	if reopened.attempts != 0 || reopened.owner.Valid {
		t.Fatalf("changed-fence replay left attempt_count = %d lease_owner valid = %v, want 0 and NULL",
			reopened.attempts, reopened.owner.Valid)
	}
}

// prove10972FencedReplayReopensAbsentFenceOnce covers the fail-open arm the
// seeded-fence test cannot: a succeeded row with no stored fence (legacy or
// unfenced-path rows) reopens exactly once on the first fenced replay, and a
// second same-fence call is a no-op — status, fence, and clocks all hold while
// still reporting scheduled.
func prove10972FencedReplayReopensAbsentFenceOnce(t *testing.T, ctx context.Context, db *sql.DB, scope, gen string) {
	t.Helper()
	queue := NewReducerQueue(SQLDB{DB: db}, "fenced-replay-absent-fence-10972", time.Minute)
	queue.MaxAttempts = 10
	queue.ClaimDomain = reducer.DomainWorkloadMaterialization
	entityKey := "repo:workload-10972-absent-fence"
	repoID := "repository:r_10972_absent"
	intent := runtime.ReducerIntent{
		ScopeID:      scope,
		GenerationID: gen,
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    entityKey,
		Reason:       "unstamped re-drive proof",
		SourceSystem: "reducer",
		Payload:      map[string]any{"probe": "absent-fence"},
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := queue.Ack(ctx, claimed, reducercontract.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	succeeded := readQueueRow10972(t, ctx, db, claimed.IntentID)
	if succeeded.status != "succeeded" || succeeded.fence != "" {
		t.Fatalf("after ACK status = %q fence = %q, want succeeded with no stored fence", succeeded.status, succeeded.fence)
	}

	replayed, err := queue.ReplayWorkloadMaterializationForFence(ctx, scope, gen, entityKey, repoID, "fence-x")
	if err != nil || !replayed {
		t.Fatalf("first fenced replay = (%v, %v), want (true, nil): an absent stored fence must reopen", replayed, err)
	}
	reopened := readQueueRow10972(t, ctx, db, claimed.IntentID)
	if reopened.status != "pending" {
		t.Fatalf("first fenced replay left status = %q, want pending", reopened.status)
	}
	if reopened.fence != "fence-x" {
		t.Fatalf("first fenced replay left fence = %q, want fence-x", reopened.fence)
	}
	if reopened.attempts != 0 || reopened.owner.Valid {
		t.Fatalf("first fenced replay left attempt_count = %d lease_owner valid = %v, want 0 and NULL",
			reopened.attempts, reopened.owner.Valid)
	}

	replayed, err = queue.ReplayWorkloadMaterializationForFence(ctx, scope, gen, entityKey, repoID, "fence-x")
	if err != nil || !replayed {
		t.Fatalf("second same-fence replay = (%v, %v), want (true, nil)", replayed, err)
	}
	held := readQueueRow10972(t, ctx, db, claimed.IntentID)
	if held.status != "pending" || held.fence != "fence-x" {
		t.Fatalf("second same-fence replay left status = %q fence = %q, want pending/fence-x", held.status, held.fence)
	}
	if !held.updatedAt.Equal(reopened.updatedAt) {
		t.Fatalf("second same-fence replay moved updated_at from %v to %v: the repeat must be a no-op",
			reopened.updatedAt, held.updatedAt)
	}
	if held.reopenedAt != reopened.reopenedAt {
		t.Fatalf("second same-fence replay moved reopened_at from %v to %v: the repeat must be a no-op",
			reopened.reopenedAt, held.reopenedAt)
	}
}
