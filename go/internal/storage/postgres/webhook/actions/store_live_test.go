// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package actionsstore_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9146 PostgreSQL 18.6 Actions inbox proof: immutable delivery conflicts, disjoint claims, expired and fenced ACKs, blocked retry budget, terminal exhaustion, pending repository lookup, and migration/bootstrap DDL parity in isolated schemas; focused local tests passed in 0.14s
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	actionsstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook/actions"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func localActionsStore(t *testing.T) (*actionsstore.Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN for live Postgres proof")
	}
	ctx := t.Context()
	boot, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = boot.Close() })
	schema := fmt.Sprintf("cicd_actions_test_%d", time.Now().UnixNano())
	if _, err = boot.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, cleanupErr := boot.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); cleanupErr != nil {
			t.Errorf("drop isolated actions schema: %v", cleanupErr)
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("options", "-c search_path="+schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := actionsstore.NewStore(postgres.SQLDB{DB: db})
	if err = store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	return store, db
}

func TestActionsDeliveryLeaseAndIdentityLive(t *testing.T) {
	store, db := localActionsStore(t)
	ctx := t.Context()
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	first := webhook.ActionsTrigger{DeliveryID: "delivery-one", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "example/repo", RunID: "123"}
	_, err := store.Store(ctx, first, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Store(ctx, first, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	changed := first
	changed.RunID = "124"
	if _, err = store.Store(ctx, changed, now); err == nil || !strings.Contains(err.Error(), "conflicting delivery identity") {
		t.Fatalf("conflicting delivery error=%v", err)
	}
	claimed, err := store.Claim(ctx, "worker", now, 1, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	newer := first
	newer.DeliveryID = "delivery-two"
	newer.RunID = "125"
	if _, err = store.Store(ctx, newer, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if acked, err := store.Ack(ctx, claimed[0], now.Add(2*time.Minute)); err != nil || acked {
		t.Fatalf("expired ACK before reap=%v err=%v", acked, err)
	}
	count, err := store.Reap(ctx, now.Add(2*time.Minute), 10)
	if err != nil || count.Requeued != 1 || count.Exhausted != 0 {
		t.Fatalf("reap=%+v err=%v", count, err)
	}
	againBatch, err := store.Claim(ctx, "worker-2", now.Add(2*time.Minute), 2, time.Minute)
	if err != nil || len(againBatch) != 2 {
		t.Fatalf("reclaim=%+v err=%v", againBatch, err)
	}
	if againBatch[0].DeliveryID != first.DeliveryID {
		t.Fatalf("handoff order=%+v, want oldest receipt first", againBatch)
	}
	var reclaimed, distinct webhook.StoredActionsTrigger
	for _, candidate := range againBatch {
		if candidate.DeliveryID == first.DeliveryID {
			reclaimed = candidate
		} else {
			distinct = candidate
		}
	}
	if reclaimed.DeliveryID == "" || distinct.DeliveryID != newer.DeliveryID {
		t.Fatalf("claimed batch=%+v", againBatch)
	}
	if reclaimed.ClaimFencingToken != claimed[0].ClaimFencingToken+1 {
		t.Fatalf("fence old=%d new=%d", claimed[0].ClaimFencingToken, reclaimed.ClaimFencingToken)
	}
	if acked, err := store.Ack(ctx, claimed[0], now.Add(2*time.Minute)); err != nil || acked {
		t.Fatalf("stale ACK=%v err=%v", acked, err)
	}
	if acked, err := store.Ack(ctx, reclaimed, now.Add(2*time.Minute)); err != nil || !acked {
		t.Fatalf("current ACK=%v err=%v", acked, err)
	}
	_, err = store.Store(ctx, first, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.QueryRowContext(ctx, `SELECT status FROM cicd_actions_deliveries WHERE delivery_id=$1`, first.DeliveryID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "handed_off" {
		t.Fatalf("duplicate reopened status=%q", status)
	}
	if err = db.QueryRowContext(ctx, `SELECT status FROM cicd_actions_deliveries WHERE delivery_id=$1`, newer.DeliveryID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "claimed" {
		t.Fatalf("distinct delivery during claim status=%q", status)
	}
}

func TestPendingRepositoryIncludesDeferredDeliveryLive(t *testing.T) {
	store, db := localActionsStore(t)
	ctx := t.Context()
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	trigger := webhook.ActionsTrigger{DeliveryID: "deferred", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "example/repo", RunID: "123"}
	if _, err := store.Store(ctx, trigger, now); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(ctx, "worker", now, 1, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	if ok, err := store.Release(ctx, claimed[0], now.Add(time.Second), "blocked"); err != nil || !ok {
		t.Fatalf("release=%v err=%v", ok, err)
	}
	pending, err := store.HasPendingRepository(ctx, "example/repo")
	if err != nil || !pending {
		t.Fatalf("deferred pending=%v err=%v", pending, err)
	}
	if pending, err := store.HasPendingRepository(ctx, "different/repo"); err != nil || pending {
		t.Fatalf("other pending=%v err=%v", pending, err)
	}
	claimed, err = store.Claim(ctx, "worker", now.Add(31*time.Second), 1, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("reclaim=%+v err=%v", claimed, err)
	}
	if ok, err := store.Ack(ctx, claimed[0], now.Add(32*time.Second)); err != nil || !ok {
		t.Fatalf("ack=%v err=%v", ok, err)
	}
	if pending, err := store.HasPendingRepository(ctx, "example/repo"); err != nil || pending {
		t.Fatalf("completed pending=%v err=%v", pending, err)
	}
	var failureClass string
	if err := db.QueryRowContext(ctx, `SELECT failure_class FROM cicd_actions_deliveries WHERE delivery_id=$1`, trigger.DeliveryID).Scan(&failureClass); err != nil {
		t.Fatal(err)
	}
	if failureClass != "" {
		t.Fatalf("handed-off delivery retains failure_class=%q", failureClass)
	}
}

func TestActionsDeliveryMigrationMatchesBootstrapSchema(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/236_cicd_actions_deliveries.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(migration)
	start := strings.Index(sql, "CREATE TABLE")
	if start < 0 {
		t.Fatal("migration has no CREATE TABLE")
	}
	if got, want := strings.TrimSpace(sql[start:]), strings.TrimSpace(actionsstore.SchemaSQL()); got != want {
		t.Fatalf("migration and EnsureSchema DDL differ\nmigration:\n%s\nbootstrap:\n%s", got, want)
	}
}

func TestActionsDeliveryConcurrentClaimsAreDisjointLive(t *testing.T) {
	store, database := localActionsStore(t)
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	for index := 0; index < 10; index++ {
		trigger := webhook.ActionsTrigger{DeliveryID: fmt.Sprintf("concurrent-%d", index), EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "example/repo", RunID: fmt.Sprint(index + 1)}
		if _, err := store.Store(t.Context(), trigger, now); err != nil {
			t.Fatal(err)
		}
	}
	type result struct {
		claims []webhook.StoredActionsTrigger
		err    error
	}
	output := make(chan result, 2)
	for worker := 0; worker < 2; worker++ {
		go func() {
			claims, err := store.Claim(context.Background(), "worker", now, 5, time.Minute)
			output <- result{claims, err}
		}()
	}
	seen := make(map[string]bool)
	batchKeys := make(map[string]bool)
	for worker := 0; worker < 2; worker++ {
		claimed := <-output
		if claimed.err != nil || len(claimed.claims) != 5 {
			t.Fatalf("worker claim=%+v err=%v", claimed.claims, claimed.err)
		}
		key := claimed.claims[0].HandoffKey
		if key == "" || batchKeys[key] {
			t.Fatalf("concurrent claim key %q is empty or reused", key)
		}
		batchKeys[key] = true
		for _, claim := range claimed.claims {
			if claim.HandoffKey != key {
				t.Fatalf("one claim returned multiple same-repository keys: %q and %q", key, claim.HandoffKey)
			}
			if seen[claim.DeliveryID] {
				t.Fatalf("duplicate concurrent claim %q", claim.DeliveryID)
			}
			seen[claim.DeliveryID] = true
		}
	}
	if len(seen) != 10 {
		t.Fatalf("claimed %d deliveries, want 10", len(seen))
	}
	if _, err := store.Store(t.Context(), webhook.ActionsTrigger{DeliveryID: "unexpired-new", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "other/repo", RunID: "99"}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if future, err := store.Claim(t.Context(), "future", now.Add(2*time.Minute), 1, 10*time.Minute); err != nil || len(future) != 1 {
		t.Fatalf("future claim=%+v err=%v", future, err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE cicd_actions_deliveries SET claim_attempts=8 WHERE delivery_id IN ('concurrent-0','concurrent-2','concurrent-4','concurrent-6','concurrent-8')`); err != nil {
		t.Fatal(err)
	}
	type reapResult struct {
		counts webhook.ActionsReapResult
		err    error
	}
	reaped := make(chan reapResult, 2)
	for range 2 {
		go func() {
			counts, err := store.Reap(context.Background(), now.Add(6*time.Minute), 100)
			reaped <- reapResult{counts, err}
		}()
	}
	var total webhook.ActionsReapResult
	for range 2 {
		result := <-reaped
		if result.err != nil {
			t.Fatal(result.err)
		}
		total.Requeued += result.counts.Requeued
		total.Exhausted += result.counts.Exhausted
	}
	if total.Requeued != 5 || total.Exhausted != 5 {
		t.Fatalf("two reapers counted %+v, want 5 requeued and 5 exhausted", total)
	}
	if again, err := store.Reap(t.Context(), now.Add(6*time.Minute), 100); err != nil || again != (webhook.ActionsReapResult{}) {
		t.Fatalf("repeated reap=%+v err=%v", again, err)
	}
	var failed, unexpired int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM cicd_actions_deliveries WHERE status='failed' AND failure_class='attempts_exhausted'`).Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM cicd_actions_deliveries WHERE delivery_id='unexpired-new' AND status='claimed'`).Scan(&unexpired); err != nil {
		t.Fatal(err)
	}
	if failed != 5 || unexpired != 1 {
		t.Fatalf("durable failures=%d unexpired=%d", failed, unexpired)
	}
}

func TestActionsClaimPersistsBoundedRepositoryBatchLive(t *testing.T) {
	store, database := localActionsStore(t)
	ctx := t.Context()
	at := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	for index := range 100 {
		trigger := webhook.ActionsTrigger{
			DeliveryID: fmt.Sprintf("burst-%03d", index), EventKind: "workflow_job",
			Action: "completed", RepositoryID: "42", Repository: "example/repo",
			RunID: "123", JobID: fmt.Sprint(index + 1),
		}
		if _, err := store.Store(ctx, trigger, at); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.Claim(ctx, "first", at, 100, time.Minute)
	if err != nil || len(first) != 100 {
		t.Fatalf("initial claim count=%d err=%v", len(first), err)
	}
	var initialKey string
	for _, claim := range first {
		var key string
		if err := database.QueryRowContext(ctx, `SELECT handoff_key FROM cicd_actions_deliveries WHERE delivery_id=$1`, claim.DeliveryID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if key == "" {
			t.Fatal("claim returned an unassigned delivery")
		}
		if claim.HandoffKey != key {
			t.Fatalf("claim key=%q, persisted key=%q", claim.HandoffKey, key)
		}
		if initialKey == "" {
			initialKey = key
		} else if key != initialKey {
			t.Fatalf("same-repository burst has keys %q and %q", initialKey, key)
		}
	}
	if ok, err := store.Ack(ctx, first[0], at.Add(time.Second)); err != nil || !ok {
		t.Fatalf("partial ACK=%v err=%v", ok, err)
	}
	if ok, err := store.Release(ctx, first[1], at.Add(time.Second), "blocked"); err != nil || !ok {
		t.Fatalf("partial release=%v err=%v", ok, err)
	}
	for _, trigger := range []webhook.ActionsTrigger{
		{DeliveryID: "late", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "example/repo", RunID: "124"},
		{DeliveryID: "same-name-different-id", EventKind: "workflow_run", Action: "completed", RepositoryID: "99", Repository: "example/repo", RunID: "125"},
		{DeliveryID: "different-name", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "example/other", RunID: "126"},
	} {
		if _, err := store.Store(ctx, trigger, at.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	late, err := store.Claim(ctx, "second", at.Add(2*time.Second), 100, time.Minute)
	if err != nil || len(late) != 3 {
		t.Fatalf("late claim count=%d err=%v", len(late), err)
	}
	seen := make(map[string]bool)
	for _, claim := range late {
		var key string
		if err := database.QueryRowContext(ctx, `SELECT handoff_key FROM cicd_actions_deliveries WHERE delivery_id=$1`, claim.DeliveryID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if key == "" || key == initialKey || seen[key] {
			t.Fatalf("late/different repository delivery %q has non-distinct key %q", claim.DeliveryID, key)
		}
		if claim.HandoffKey != key {
			t.Fatalf("late claim key=%q, persisted key=%q", claim.HandoffKey, key)
		}
		seen[key] = true
	}
	retried, err := store.Claim(ctx, "retry", at.Add(32*time.Second), 100, time.Minute)
	if err != nil || len(retried) != 1 || retried[0].DeliveryID != first[1].DeliveryID {
		t.Fatalf("partial retry=%+v err=%v", retried, err)
	}
	var retryKey string
	if err := database.QueryRowContext(ctx, `SELECT handoff_key FROM cicd_actions_deliveries WHERE delivery_id=$1`, retried[0].DeliveryID).Scan(&retryKey); err != nil {
		t.Fatal(err)
	}
	if retryKey != initialKey || retried[0].HandoffKey != initialKey {
		t.Fatalf("reclaimed key=%q, want immutable %q", retryKey, initialKey)
	}
}

func TestActionsDeliveryBlockedBudgetAndFailedRetryCapLive(t *testing.T) {
	store, db := localActionsStore(t)
	ctx := t.Context()
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	trigger := webhook.ActionsTrigger{DeliveryID: "retry-budget", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "example/repo", RunID: "123"}
	if _, err := store.Store(ctx, trigger, now); err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 12; iteration++ {
		at := now.Add(time.Duration(iteration) * 31 * time.Second)
		claimed, err := store.Claim(ctx, "worker", at, 1, time.Minute)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("blocked iteration %d claim=%+v err=%v", iteration, claimed, err)
		}
		if ok, err := store.Release(ctx, claimed[0], at.Add(time.Second), "blocked"); err != nil || !ok {
			t.Fatalf("blocked iteration %d release=%v err=%v", iteration, ok, err)
		}
	}
	for iteration := 0; iteration < 8; iteration++ {
		at := now.Add(13*time.Minute + time.Duration(iteration)*time.Second)
		claimed, err := store.Claim(ctx, "worker", at, 1, time.Minute)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("failed iteration %d claim=%+v err=%v", iteration, claimed, err)
		}
		if ok, err := store.Release(ctx, claimed[0], at.Add(time.Millisecond), "admission_failed"); err != nil || !ok {
			t.Fatalf("failed iteration %d release=%v err=%v", iteration, ok, err)
		}
	}
	var status, failure string
	if err := db.QueryRowContext(ctx, `SELECT status,failure_class FROM cicd_actions_deliveries WHERE delivery_id=$1`, trigger.DeliveryID).Scan(&status, &failure); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || failure != "attempts_exhausted" {
		t.Fatalf("terminal status=%q failure=%q", status, failure)
	}
}
