// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestAdminStoreIdentityFenceMutationsLive exercises the admin UPDATEs against
// the identity cutover checks. Temporary tables keep this proof off durable data.
func TestAdminStoreIdentityFenceMutationsLive(t *testing.T) {
	if os.Getenv("ESHU_ADMIN_REPLAY_FENCE_LIVE") != "1" {
		t.Skip("set ESHU_ADMIN_REPLAY_FENCE_LIVE=1 with an isolated Postgres DSN")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("ESHU_POSTGRES_DSN is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, `
CREATE TEMP TABLE ingestion_scopes (scope_id text PRIMARY KEY, source_key text NOT NULL);
CREATE TEMP TABLE fact_work_items (
    work_item_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
    stage text NOT NULL, domain text NOT NULL, status text NOT NULL,
    attempt_count integer NOT NULL, lease_owner text, claim_until timestamptz,
    visible_at timestamptz, next_attempt_at timestamptz, last_attempt_at timestamptz,
    failure_class text, failure_message text, failure_details text,
    created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
    container_image_identity_claim_epoch bigint NOT NULL,
    container_image_identity_v2_required boolean NOT NULL,
    container_image_identity_v2_authorized_status text NOT NULL,
    container_image_identity_v3_required boolean NOT NULL,
    container_image_identity_v3_authorized_status text NOT NULL,
    CONSTRAINT fact_work_items_container_image_identity_v2_status_check
        CHECK (NOT container_image_identity_v2_required OR status = container_image_identity_v2_authorized_status),
    CONSTRAINT fact_work_items_container_image_identity_v3_status_check
        CHECK (NOT container_image_identity_v3_required OR status = container_image_identity_v3_authorized_status)
);
CREATE TEMP TABLE fact_replay_events (
    replay_event_id text PRIMARY KEY, work_item_id text NOT NULL,
    scope_id text NOT NULL, generation_id text NOT NULL,
    failure_class text, operator_note text, created_at timestamptz NOT NULL
);
SET search_path TO pg_temp;
INSERT INTO ingestion_scopes VALUES
    ('replay-scope', 'replay-repo'), ('dead-scope', 'dead-repo');
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    last_attempt_at, failure_class, failure_message, created_at, updated_at,
    container_image_identity_claim_epoch,
    container_image_identity_v2_required, container_image_identity_v2_authorized_status,
    container_image_identity_v3_required, container_image_identity_v3_authorized_status
) VALUES
    ('replay-both', 'replay-scope', 'gen', 'reducer', 'container_image_identity', 'dead_letter', 3,
     '2026-09-18T00:00:00Z', 'projection_bug', 'old FIPS failure', now(), now(), 7, true, 'dead_letter', true, 'dead_letter'),
    ('replay-v2', 'replay-scope', 'gen', 'reducer', 'container_image_identity', 'dead_letter', 3,
     '2026-09-18T00:00:00Z', 'projection_bug', 'old FIPS failure', now(), now(), 7, true, 'dead_letter', false, ''),
    ('replay-v3', 'replay-scope', 'gen', 'reducer', 'container_image_identity', 'dead_letter', 3,
     '2026-09-18T00:00:00Z', 'projection_bug', 'old FIPS failure', now(), now(), 7, false, '', true, 'dead_letter'),
    ('replay-plain', 'replay-scope', 'gen', 'reducer', 'container_image_identity', 'dead_letter', 3,
     '2026-09-18T00:00:00Z', 'projection_bug', 'old FIPS failure', now(), now(), 7, false, '', false, ''),
    ('replay-succeeded', 'replay-scope', 'gen', 'reducer', 'container_image_identity', 'succeeded', 3,
     '2026-09-18T00:00:00Z', null, null, now(), now(), 7, true, 'succeeded', true, 'succeeded'),
    ('dead-failed', 'dead-scope', 'gen', 'reducer', 'container_image_identity', 'failed', 3,
     '2026-09-18T00:00:00Z', 'projection_bug', 'old failure', now(), now(), 7, true, 'failed', true, 'failed');
`); err != nil {
		t.Fatalf("seed temporary identity work items: %v", err)
	}

	adminStore := store.NewStore(db)
	replayIDs := []string{"replay-both", "replay-v2", "replay-v3", "replay-plain", "replay-succeeded"}
	items, err := adminStore.ReplayFailedWorkItems(ctx, admin.ReplayWorkItemFilter{
		WorkItemIDs: replayIDs, Stage: "reducer", FailureClass: "projection_bug",
		OperatorNote: "fixed FIPS query", Limit: len(replayIDs),
	})
	if err != nil {
		t.Fatalf("replay guarded dead letters: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("replayed %d items, want four terminal rows and no succeeded row", len(items))
	}
	for _, test := range []struct{ id, v2, v3 string }{
		{"replay-both", "pending", "pending"},
		{"replay-v2", "pending", ""},
		{"replay-v3", "", "pending"},
		{"replay-plain", "", ""},
		{"replay-succeeded", "succeeded", "succeeded"},
	} {
		wantStatus := "pending"
		if test.id == "replay-succeeded" {
			wantStatus = "succeeded"
		}
		checkIdentityWorkItem(t, ctx, db, test.id, wantStatus, test.v2, test.v3)
	}
	items, err = adminStore.ReplayFailedWorkItems(ctx, admin.ReplayWorkItemFilter{
		WorkItemIDs: replayIDs, Stage: "reducer", FailureClass: "projection_bug", Limit: len(replayIDs),
	})
	if err != nil || len(items) != 0 {
		t.Fatalf("repeat replay = %d items, %v; want empty and no error", len(items), err)
	}

	items, err = adminStore.DeadLetterWorkItems(ctx, admin.DeadLetterFilter{WorkItemIDs: []string{"dead-failed"}})
	if err != nil || len(items) != 1 {
		t.Fatalf("dead-letter guarded failure = %d items, %v", len(items), err)
	}
	checkIdentityWorkItem(t, ctx, db, "dead-failed", "dead_letter", "dead_letter", "dead_letter")

	var eventCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM fact_replay_events").Scan(&eventCount); err != nil || eventCount != 4 {
		t.Fatalf("replay events = %d, %v; want four", eventCount, err)
	}
}

func checkIdentityWorkItem(t *testing.T, ctx context.Context, db *sql.DB, id, status, v2, v3 string) {
	t.Helper()
	var gotStatus, gotV2, gotV3 string
	var attempts int
	var epoch int64
	var lastAttempt time.Time
	err := db.QueryRowContext(ctx, `
SELECT status, container_image_identity_v2_authorized_status,
       container_image_identity_v3_authorized_status, attempt_count,
       container_image_identity_claim_epoch, last_attempt_at
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&gotStatus, &gotV2, &gotV3, &attempts, &epoch, &lastAttempt)
	if err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || gotV2 != v2 || gotV3 != v3 || attempts != 3 || epoch != 7 ||
		!lastAttempt.Equal(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("%s: status/auth=%s/%s/%s attempts=%d epoch=%d last_attempt=%s",
			id, gotStatus, gotV2, gotV3, attempts, epoch, lastAttempt)
	}
}

// TestAdminStoreTerminalMutationsRecheckStatusLive proves a row claimed after
// selection cannot be reset by a competing admin replay or dead-letter action.
func TestAdminStoreTerminalMutationsRecheckStatusLive(t *testing.T) {
	for _, deadLetter := range []bool{false, true} {
		name := "replay"
		if deadLetter {
			name = "dead-letter"
		}
		t.Run(name, func(t *testing.T) { runAdminStoreTerminalRecheckLive(t, deadLetter) })
	}
}

func runAdminStoreTerminalRecheckLive(t *testing.T, deadLetter bool) {
	if os.Getenv("ESHU_ADMIN_REPLAY_FENCE_LIVE") != "1" {
		t.Skip("set ESHU_ADMIN_REPLAY_FENCE_LIVE=1 with an isolated Postgres DSN")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "::1") {
		t.Fatal("contention proof requires an isolated loopback Postgres DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = adminDB.Close() }()
	schema := fmt.Sprintf("replay_fence_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop replay-fence test schema %s: %v", schema, err)
		}
	}()
	if _, err := adminDB.ExecContext(ctx, `
CREATE TABLE `+schema+`.fact_work_items (
    work_item_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
    stage text NOT NULL, domain text NOT NULL, status text NOT NULL,
    attempt_count integer NOT NULL, lease_owner text, claim_until timestamptz,
    visible_at timestamptz, next_attempt_at timestamptz,
    failure_class text, failure_message text, failure_details text,
    created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
    container_image_identity_v2_required boolean NOT NULL,
    container_image_identity_v2_authorized_status text NOT NULL,
    container_image_identity_v3_required boolean NOT NULL,
    container_image_identity_v3_authorized_status text NOT NULL,
    CHECK (NOT container_image_identity_v2_required OR status = container_image_identity_v2_authorized_status),
    CHECK (NOT container_image_identity_v3_required OR status = container_image_identity_v3_authorized_status)
);
CREATE TABLE `+schema+`.fact_replay_events (
    replay_event_id text PRIMARY KEY, work_item_id text NOT NULL,
    scope_id text NOT NULL, generation_id text NOT NULL,
    failure_class text, operator_note text, created_at timestamptz NOT NULL
);
INSERT INTO `+schema+`.fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, failure_class, failure_message, created_at, updated_at,
    container_image_identity_v2_required, container_image_identity_v2_authorized_status,
    container_image_identity_v3_required, container_image_identity_v3_authorized_status
) VALUES ('race', 'scope', 'generation', 'reducer', 'container_image_identity',
          'dead_letter', 3, 'projection_bug', 'old FIPS failure', now(), now(),
          true, 'dead_letter', true, 'dead_letter');`); err != nil {
		t.Fatal(err)
	}

	claimDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = claimDB.Close() }()
	claimDB.SetMaxOpenConns(1)
	replayDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = replayDB.Close() }()
	replayDB.SetMaxOpenConns(1)
	for _, db := range []*sql.DB{claimDB, replayDB} {
		if _, err := db.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
			t.Fatal(err)
		}
	}
	var replayPID int
	if err := replayDB.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&replayPID); err != nil {
		t.Fatal(err)
	}
	claimTx, err := claimDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = claimTx.Rollback() }()
	if _, err := claimTx.ExecContext(ctx, `UPDATE fact_work_items
SET status='claimed', container_image_identity_v2_authorized_status='claimed',
    container_image_identity_v3_authorized_status='claimed',
    lease_owner='worker', claim_until=now()+interval '10 minutes'
WHERE work_item_id='race'`); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		count int
		err   error
	}
	finished := make(chan outcome, 1)
	go func() {
		adminStore := store.NewStore(replayDB)
		var items []admin.WorkItem
		var mutationErr error
		if deadLetter {
			items, mutationErr = adminStore.DeadLetterWorkItems(ctx, admin.DeadLetterFilter{
				WorkItemIDs: []string{"race"}, Stage: "reducer", FailureClass: "projection_bug", Limit: 1,
			})
		} else {
			items, mutationErr = adminStore.ReplayFailedWorkItems(ctx, admin.ReplayWorkItemFilter{
				WorkItemIDs: []string{"race"}, Stage: "reducer", FailureClass: "projection_bug", Limit: 1,
			})
		}
		finished <- outcome{count: len(items), err: mutationErr}
	}()
	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var wait sql.NullString
		if err := adminDB.QueryRowContext(ctx, "SELECT wait_event_type FROM pg_stat_activity WHERE pid=$1", replayPID).Scan(&wait); err != nil {
			t.Fatal(err)
		}
		if wait.Valid && wait.String == "Lock" {
			waiting = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("admin mutation never waited for the competing claim row lock")
	}
	if err := claimTx.Commit(); err != nil {
		t.Fatal(err)
	}
	result := <-finished
	if result.err != nil || result.count != 0 {
		t.Fatalf("admin mutation after competing claim = %d items, %v; want no mutation", result.count, result.err)
	}
	var status, owner string
	if err := claimDB.QueryRowContext(ctx, "SELECT status,lease_owner FROM fact_work_items WHERE work_item_id='race'").Scan(&status, &owner); err != nil {
		t.Fatal(err)
	}
	if status != "claimed" || owner != "worker" {
		t.Fatalf("competing claim was stolen: status=%s owner=%s", status, owner)
	}
}
