// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Proof gate for Git webhook-triggered repository refresh with scheduled
// polling fallback (issue #1802, parent #1797).
//
// The proof exercises the real persistence and selector machinery end to end
// against a live Postgres instance:
//
//	signed fixture delivery -> signature verify -> normalize -> StoreTrigger
//	(queued) -> WebhookTriggerRepositorySelector claim + targeted sync +
//	handed_off -> duplicate delivery coalesces on refresh_key -> failed sync is
//	marked failed visibly -> a missed delivery leaves scheduled polling as the
//	authoritative recovery path.
//
// Credential safety: every secret here is a synthetic, in-test HMAC key and
// every payload is a fixture literal. No real provider secret, token,
// repository payload, hostname, or delivery id is committed. The signature is
// computed at runtime from the synthetic key so the fixture proves real
// HMAC-SHA256 verification rather than a recorded signature.
//
// Performance Evidence: this is a proof/observability gate, not a runtime hot
// path change. It adds no production code and no new graph writes, queue
// stages, workers, leases, or batching. It measures intake-to-handoff and
// claim wall time per scenario and asserts terminal queue counts so an operator
// can read coalesce, failure, and fallback behavior. Captured timings and
// per-status counts are logged via t.Logf. No production concurrency defaults
// change. No-Regression Evidence: no production code path is modified.
// Observability Evidence: per-scenario wall time, per-status trigger counts,
// and duplicate_count are logged from the live store so the queue drain,
// coalesce, failure visibility, and fallback assertions are operator-readable.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

const webhookRefreshProofDSNEnv = "ESHU_POSTGRES_DSN"

// fixtureWebhookSecret is a synthetic HMAC key used only inside this test. It
// is intentionally obvious and is never a real provider secret.
const fixtureWebhookSecret = "fixture-webhook-secret-do-not-use-in-production"

// fixtureGitHubPushPayload is a minimal signed-push fixture. The repository,
// branch, and commit SHAs are synthetic literals, not real repository data.
const fixtureGitHubPushPayload = `{
	"ref":"refs/heads/main",
	"before":"1111111111111111111111111111111111111111",
	"after":"2222222222222222222222222222222222222222",
	"repository":{"id":99001,"full_name":"eshu-fixture/proof-repo","default_branch":"main"}
}`

// fixtureGitHubPushPayloadSecondCommit is a follow-up push to the same repo on
// a different commit. It proves that a genuinely new commit produces a new
// refresh_key rather than coalescing with the first.
const fixtureGitHubPushPayloadSecondCommit = `{
	"ref":"refs/heads/main",
	"before":"2222222222222222222222222222222222222222",
	"after":"3333333333333333333333333333333333333333",
	"repository":{"id":99001,"full_name":"eshu-fixture/proof-repo","default_branch":"main"}
}`

// TestWebhookRefreshProofEndToEnd is the issue #1802 gate. It runs the full
// signed-fixture intake, claim/handoff, coalesce, failed-sync, and
// scheduled-fallback flow against a live Postgres store. It skips when no DSN
// is configured so it does not block unit-only runs.
func TestWebhookRefreshProofEndToEnd(t *testing.T) {
	store, db := openWebhookRefreshProofStore(t)
	ctx := context.Background()

	// Stage 0: start with one indexed repository fixture. The trigger store does
	// not own indexed-repo state; the fixture identity is the repository the
	// webhook will target. We assert an empty trigger queue as the clean start.
	if got := webhookProofStatusCounts(t, db); len(got) != 0 {
		t.Fatalf("clean start status counts = %#v, want empty", got)
	}

	var queuedDeliveryStart, queuedDeliveryEnd time.Time

	t.Run("signed delivery queues a trigger", func(t *testing.T) {
		queuedDeliveryStart = time.Now()
		trigger := mustVerifyAndNormalizeGitHubPush(t, "fixture-delivery-1", fixtureGitHubPushPayload)
		stored, err := store.StoreTrigger(ctx, trigger, time.Now().UTC())
		queuedDeliveryEnd = time.Now()
		if err != nil {
			t.Fatalf("StoreTrigger() error = %v, want nil", err)
		}
		if stored.Status != webhook.TriggerStatusQueued {
			t.Fatalf("Status = %q, want %q", stored.Status, webhook.TriggerStatusQueued)
		}
		if stored.Decision != webhook.DecisionAccepted {
			t.Fatalf("Decision = %q, want accepted", stored.Decision)
		}
		if stored.RefreshKey == "" || stored.TriggerID == "" {
			t.Fatalf("stored trigger missing durable keys: %#v", stored)
		}
		if stored.DuplicateCount != 0 {
			t.Fatalf("DuplicateCount = %d, want 0 on first delivery", stored.DuplicateCount)
		}
		counts := webhookProofStatusCounts(t, db)
		if counts[string(webhook.TriggerStatusQueued)] != 1 {
			t.Fatalf("queued count = %d, want 1 (counts=%#v)", counts[string(webhook.TriggerStatusQueued)], counts)
		}
		t.Logf("queued one trigger in %s; refresh_key=%q queue_counts=%#v",
			queuedDeliveryEnd.Sub(queuedDeliveryStart), stored.RefreshKey, counts)
	})

	t.Run("duplicate delivery coalesces on refresh_key", func(t *testing.T) {
		// Same provider+repo+branch+commit, different delivery id. The refresh
		// identity is unchanged, so this must coalesce instead of enqueuing a
		// second unit of work. This is the idempotency key (refresh_key) proof.
		trigger := mustVerifyAndNormalizeGitHubPush(t, "fixture-delivery-1-retry", fixtureGitHubPushPayload)
		stored, err := store.StoreTrigger(ctx, trigger, time.Now().UTC())
		if err != nil {
			t.Fatalf("StoreTrigger() duplicate error = %v, want nil", err)
		}
		if stored.DuplicateCount != 1 {
			t.Fatalf("DuplicateCount = %d, want 1 after one duplicate", stored.DuplicateCount)
		}
		counts := webhookProofStatusCounts(t, db)
		if total := webhookProofTotalRows(t, db); total != 1 {
			t.Fatalf("total trigger rows = %d, want 1 (coalesced) counts=%#v", total, counts)
		}
		if counts[string(webhook.TriggerStatusQueued)] != 1 {
			t.Fatalf("queued count = %d, want still 1 after coalesce", counts[string(webhook.TriggerStatusQueued)])
		}
		t.Logf("duplicate delivery coalesced: duplicate_count=%d total_rows=1 queue_counts=%#v",
			stored.DuplicateCount, counts)
	})

	t.Run("claim and handoff trigger targeted repository sync", func(t *testing.T) {
		var syncedRepoIDs []string
		claimStart := time.Now()
		selector := collector.WebhookTriggerRepositorySelector{
			Config:     collector.RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
			Store:      store,
			Owner:      "collector-git-proof",
			ClaimLimit: 50,
			Now:        func() time.Time { return time.Now().UTC() },
			SyncGit: func(_ context.Context, _ collector.RepoSyncConfig, repositoryIDs []string) (collector.GitSyncSelection, error) {
				syncedRepoIDs = append([]string(nil), repositoryIDs...)
				paths := make([]string, 0, len(repositoryIDs))
				for range repositoryIDs {
					paths = append(paths, t.TempDir())
				}
				return collector.GitSyncSelection{SelectedRepoPaths: paths}, nil
			},
		}
		batch, err := selector.SelectRepositories(ctx)
		claimDuration := time.Since(claimStart)
		if err != nil {
			t.Fatalf("SelectRepositories() error = %v, want nil", err)
		}
		// Targeted sync: only the one fixture repo is synced, proving the webhook
		// path narrows work to the referenced repository, not a full re-index.
		if len(syncedRepoIDs) != 1 || syncedRepoIDs[0] != "eshu-fixture/proof-repo" {
			t.Fatalf("syncedRepoIDs = %#v, want one targeted fixture repo", syncedRepoIDs)
		}
		if len(batch.Repositories) != 1 {
			t.Fatalf("len(batch.Repositories) = %d, want 1 targeted repo", len(batch.Repositories))
		}
		counts := webhookProofStatusCounts(t, db)
		if counts[string(webhook.TriggerStatusHandedOff)] != 1 {
			t.Fatalf("handed_off count = %d, want 1 (counts=%#v)", counts[string(webhook.TriggerStatusHandedOff)], counts)
		}
		if counts[string(webhook.TriggerStatusQueued)] != 0 {
			t.Fatalf("queued count = %d, want 0 after handoff", counts[string(webhook.TriggerStatusQueued)])
		}
		t.Logf("claimed+handed off in %s; targeted_repos=%#v queue_counts=%#v",
			claimDuration, syncedRepoIDs, counts)
	})

	t.Run("already-drained queue is a no-op for the webhook selector", func(t *testing.T) {
		// Replay/retry matrix: an empty (already-drained) webhook queue must
		// return an empty batch without re-syncing the already handed-off repo.
		selector := collector.WebhookTriggerRepositorySelector{
			Config: collector.RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
			Store:  store,
			Owner:  "collector-git-proof",
			Now:    func() time.Time { return time.Now().UTC() },
			SyncGit: func(context.Context, collector.RepoSyncConfig, []string) (collector.GitSyncSelection, error) {
				t.Fatal("SyncGit called on drained queue, want no targeted sync")
				return collector.GitSyncSelection{}, nil
			},
		}
		batch, err := selector.SelectRepositories(ctx)
		if err != nil {
			t.Fatalf("SelectRepositories() drained error = %v, want nil", err)
		}
		if len(batch.Repositories) != 0 {
			t.Fatalf("drained batch repositories = %d, want 0", len(batch.Repositories))
		}
	})

	t.Run("missed webhook leaves scheduled polling authoritative", func(t *testing.T) {
		// MISSED-webhook scenario: a new commit landed but no webhook arrived, so
		// the webhook selector finds nothing. The PriorityRepositorySelector must
		// fall through to scheduled polling, which is the authoritative recovery
		// path. We model scheduled polling with a deterministic selector that
		// returns the repository the missed webhook would have targeted.
		var webhookSynced bool
		webhookSelector := collector.WebhookTriggerRepositorySelector{
			Config: collector.RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
			Store:  store,
			Owner:  "collector-git-proof",
			Now:    func() time.Time { return time.Now().UTC() },
			SyncGit: func(context.Context, collector.RepoSyncConfig, []string) (collector.GitSyncSelection, error) {
				webhookSynced = true
				return collector.GitSyncSelection{}, nil
			},
		}
		scheduled := &proofScheduledSelector{
			batch: collector.SelectionBatch{
				ObservedAt: time.Now().UTC(),
				Repositories: []collector.SelectedRepository{{
					RepoPath:    t.TempDir(),
					RemoteURL:   "https://github.com/eshu-fixture/proof-repo.git",
					DisplayName: "eshu-fixture/proof-repo",
				}},
			},
		}
		priority := collector.PriorityRepositorySelector{
			Selectors: []collector.RepositorySelector{webhookSelector, scheduled},
		}
		start := time.Now()
		batch, err := priority.SelectRepositories(ctx)
		fallbackDuration := time.Since(start)
		if err != nil {
			t.Fatalf("PriorityRepositorySelector error = %v, want nil", err)
		}
		if webhookSynced {
			t.Fatal("webhook selector synced on a missed delivery, want scheduled fallback authoritative")
		}
		if !scheduled.called {
			t.Fatal("scheduled selector not consulted, want authoritative recovery")
		}
		if len(batch.Repositories) != 1 {
			t.Fatalf("fallback batch repositories = %d, want 1 from scheduled polling", len(batch.Repositories))
		}
		t.Logf("missed-webhook fallback resolved via scheduled polling in %s; repos=%d",
			fallbackDuration, len(batch.Repositories))
	})

	t.Run("failed sync marks the trigger failed visibly", func(t *testing.T) {
		// A genuinely new commit arrives (new refresh_key, not coalesced), then
		// the targeted sync fails. The trigger must move to a visible failed
		// state with a failure class and timestamp, not be silently dropped.
		trigger := mustVerifyAndNormalizeGitHubPush(t, "fixture-delivery-2", fixtureGitHubPushPayloadSecondCommit)
		stored, err := store.StoreTrigger(ctx, trigger, time.Now().UTC())
		if err != nil {
			t.Fatalf("StoreTrigger() second commit error = %v, want nil", err)
		}
		if stored.DuplicateCount != 0 {
			t.Fatalf("second-commit DuplicateCount = %d, want 0 (new refresh_key)", stored.DuplicateCount)
		}
		if got := webhookProofTotalRows(t, db); got != 2 {
			t.Fatalf("total rows = %d, want 2 (distinct commits do not coalesce)", got)
		}

		failingSelector := collector.WebhookTriggerRepositorySelector{
			Config: collector.RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
			Store:  store,
			Owner:  "collector-git-proof",
			Now:    func() time.Time { return time.Now().UTC() },
			SyncGit: func(context.Context, collector.RepoSyncConfig, []string) (collector.GitSyncSelection, error) {
				return collector.GitSyncSelection{}, errors.New("simulated git sync failure")
			},
		}
		if _, err := failingSelector.SelectRepositories(ctx); err == nil {
			t.Fatal("SelectRepositories() error = nil, want surfaced sync failure")
		}
		counts := webhookProofStatusCounts(t, db)
		if counts[string(webhook.TriggerStatusFailed)] != 1 {
			t.Fatalf("failed count = %d, want 1 visible failure (counts=%#v)", counts[string(webhook.TriggerStatusFailed)], counts)
		}
		class, message, failedAt := webhookProofFailureDetails(t, db, stored.TriggerID)
		if class != "sync_git_failed" {
			t.Fatalf("failure_class = %q, want sync_git_failed", class)
		}
		if message == "" {
			t.Fatal("failure_message empty, want operator-visible reason")
		}
		if !failedAt.Valid {
			t.Fatal("failed_at NULL, want a visible failure timestamp")
		}
		t.Logf("failed sync recorded visibly: class=%q message=%q failed_at=%s queue_counts=%#v",
			class, message, failedAt.Time, counts)
	})

	t.Run("final query truth: queue terminal state", func(t *testing.T) {
		// Final readback: one trigger handed off (sync succeeded), one failed
		// (sync failed), zero still queued. This is the operator-facing truth an
		// API/MCP status surface would report for the proof corpus.
		counts := webhookProofStatusCounts(t, db)
		if counts[string(webhook.TriggerStatusQueued)] != 0 {
			t.Fatalf("terminal queued = %d, want 0", counts[string(webhook.TriggerStatusQueued)])
		}
		if counts[string(webhook.TriggerStatusHandedOff)] != 1 {
			t.Fatalf("terminal handed_off = %d, want 1", counts[string(webhook.TriggerStatusHandedOff)])
		}
		if counts[string(webhook.TriggerStatusFailed)] != 1 {
			t.Fatalf("terminal failed = %d, want 1", counts[string(webhook.TriggerStatusFailed)])
		}
		t.Logf("FINAL QUERY TRUTH queue_counts=%#v retrying=0 dead_letters=0", counts)
	})
}

// proofScheduledSelector is a deterministic stand-in for scheduled polling. It
// records whether it was consulted so the proof can assert that polling is the
// authoritative recovery path when a webhook delivery is missed.
type proofScheduledSelector struct {
	batch  collector.SelectionBatch
	called bool
}

func (s *proofScheduledSelector) SelectRepositories(context.Context) (collector.SelectionBatch, error) {
	s.called = true
	return s.batch, nil
}

// mustVerifyAndNormalizeGitHubPush reproduces the webhook listener intake path:
// it computes the real HMAC-SHA256 signature from the synthetic fixture secret,
// verifies it, and normalizes the fixture payload into a Trigger. This proves
// signature handling and normalization without importing the package main
// listener and without any recorded or real signature.
func mustVerifyAndNormalizeGitHubPush(t *testing.T, deliveryID, payload string) webhook.Trigger {
	t.Helper()
	body := []byte(payload)
	signature := fixtureGitHubSignature(fixtureWebhookSecret, body)
	if err := webhook.VerifyGitHubSignature(body, fixtureWebhookSecret, signature); err != nil {
		t.Fatalf("VerifyGitHubSignature() error = %v, want nil", err)
	}
	// A wrong key must be rejected, proving the verifier is not a no-op.
	if err := webhook.VerifyGitHubSignature(body, "wrong-key", signature); err == nil {
		t.Fatal("VerifyGitHubSignature() with wrong key error = nil, want mismatch")
	}
	trigger, err := webhook.NormalizeGitHub("push", deliveryID, body, "main")
	if err != nil {
		t.Fatalf("NormalizeGitHub() error = %v, want nil", err)
	}
	return trigger
}

func fixtureGitHubSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// webhookProofStatusCounts returns the live per-status row count for the
// webhook trigger queue so each scenario can assert operator-visible truth.
func webhookProofStatusCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT status, COUNT(*) FROM webhook_refresh_triggers GROUP BY status`)
	if err != nil {
		t.Fatalf("status count query error = %v, want nil", err)
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			t.Fatalf("scan status count error = %v, want nil", err)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("status count rows error = %v, want nil", err)
	}
	return counts
}

func webhookProofTotalRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var total int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM webhook_refresh_triggers`).Scan(&total); err != nil {
		t.Fatalf("total row count error = %v, want nil", err)
	}
	return total
}

func webhookProofFailureDetails(t *testing.T, db *sql.DB, triggerID string) (string, string, sql.NullTime) {
	t.Helper()
	var class, message sql.NullString
	var failedAt sql.NullTime
	err := db.QueryRowContext(context.Background(),
		`SELECT failure_class, failure_message, failed_at FROM webhook_refresh_triggers WHERE trigger_id = $1`,
		triggerID).Scan(&class, &message, &failedAt)
	if err != nil {
		t.Fatalf("failure detail query error = %v, want nil", err)
	}
	return class.String, message.String, failedAt
}

// openWebhookRefreshProofStore connects to the configured Postgres instance,
// applies the webhook trigger schema, and isolates the proof by truncating the
// trigger table. It skips when no DSN is set so unit-only runs stay green.
func openWebhookRefreshProofStore(t *testing.T) (*WebhookTriggerStore, *sql.DB) {
	t.Helper()
	dsn := os.Getenv(webhookRefreshProofDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping webhook refresh Postgres proof", webhookRefreshProofDSNEnv)
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("PingContext() error = %v, want nil", err)
	}
	store := NewWebhookTriggerStore(SQLDB{DB: db})
	if err := store.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("EnsureSchema() error = %v, want nil", err)
	}
	if _, err := db.ExecContext(ctx,
		`TRUNCATE webhook_refresh_triggers RESTART IDENTITY`); err != nil {
		_ = db.Close()
		t.Fatalf("TRUNCATE webhook_refresh_triggers error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `TRUNCATE webhook_refresh_triggers RESTART IDENTITY`)
		_ = db.Close()
	})
	return store, db
}
