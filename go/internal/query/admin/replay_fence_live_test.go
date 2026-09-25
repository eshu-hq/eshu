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
     '2026-09-18T00:00:00Z', 'projection_bug', 'old failure', now(), now(), 7, true, 'failed', true, 'failed'),
    ('dead-plain', 'dead-scope', 'gen', 'reducer', 'container_image_identity', 'failed', 3,
     '2026-09-18T00:00:00Z', 'projection_bug', 'old failure', now(), now(), 7, false, '', false, '');
`); err != nil {
		t.Fatalf("seed temporary identity work items: %v", err)
	}

	adminStore := store.NewStore(db)
	replayIDs := []string{"replay-both", "replay-v2", "replay-v3", "replay-plain", "replay-succeeded"}
	// #7120: the unsafe-target read must name exactly the terminal projection_bug
	// rows, skip the succeeded row and the unknown id, and honor the stage filter.
	targets, err := adminStore.UnsafeReplayTargets(ctx, admin.UnsafeReplayTargetFilter{
		WorkItemIDs:          append([]string{"replay-missing"}, replayIDs...),
		UnsafeFailureClasses: []string{"projection_bug", "resource_exhausted"},
	})
	if err != nil {
		t.Fatalf("read unsafe replay targets: %v", err)
	}
	if len(targets) != 4 || targets[0].WorkItemID != "replay-both" || targets[0].FailureClass != "projection_bug" {
		t.Fatalf("unsafe targets = %+v, want the four terminal projection_bug rows sorted by id", targets)
	}
	if wrongStage, err := adminStore.UnsafeReplayTargets(ctx, admin.UnsafeReplayTargetFilter{
		WorkItemIDs: replayIDs, Stage: "projector", UnsafeFailureClasses: []string{"projection_bug"},
	}); err != nil || len(wrongStage) != 0 {
		t.Fatalf("stage-mismatched unsafe read = %+v, %v; want none", wrongStage, err)
	}
	// Predicate parity with the replay: failure_class narrows the read too, so a
	// projection_bug id is not an unsafe target of a transient_error selector.
	if narrowed, err := adminStore.UnsafeReplayTargets(ctx, admin.UnsafeReplayTargetFilter{
		WorkItemIDs: replayIDs, FailureClass: "transient_error", UnsafeFailureClasses: []string{"projection_bug"},
	}); err != nil || len(narrowed) != 0 {
		t.Fatalf("failure_class-narrowed unsafe read = %+v, %v; want none", narrowed, err)
	}
	if matching, err := adminStore.UnsafeReplayTargets(ctx, admin.UnsafeReplayTargetFilter{
		WorkItemIDs: replayIDs, FailureClass: "projection_bug", UnsafeFailureClasses: []string{"projection_bug"},
	}); err != nil || len(matching) != 4 {
		t.Fatalf("failure_class-matching unsafe read = %+v, %v; want four", matching, err)
	}
	if safeOnly, err := adminStore.UnsafeReplayTargets(ctx, admin.UnsafeReplayTargetFilter{
		WorkItemIDs: replayIDs, UnsafeFailureClasses: []string{"input_invalid"},
	}); err != nil || len(safeOnly) != 0 {
		t.Fatalf("class-mismatched unsafe read = %+v, %v; want none", safeOnly, err)
	}
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

	items, err = adminStore.DeadLetterWorkItems(ctx, admin.DeadLetterFilter{WorkItemIDs: []string{"dead-failed", "dead-plain"}})
	if err != nil || len(items) != 2 {
		t.Fatalf("dead-letter guarded and plain failures = %d items, %v", len(items), err)
	}
	checkIdentityWorkItem(t, ctx, db, "dead-failed", "dead_letter", "dead_letter", "dead_letter")
	checkIdentityWorkItem(t, ctx, db, "dead-plain", "dead_letter", "", "")

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes VALUES ('skip-scope', 'skip-repo');
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    last_attempt_at, created_at, updated_at, container_image_identity_claim_epoch,
    container_image_identity_v2_required, container_image_identity_v2_authorized_status,
    container_image_identity_v3_required, container_image_identity_v3_authorized_status
) VALUES
    ('skip-both-pending', 'skip-scope', 'gen', 'reducer', 'container_image_identity', 'pending', 3,
     '2026-09-18T00:00:00Z', now(), now(), 7, true, 'pending', true, 'pending'),
    ('skip-v2-retrying', 'skip-scope', 'gen', 'reducer', 'container_image_identity', 'retrying', 3,
     '2026-09-18T00:00:00Z', now(), now(), 7, true, 'retrying', false, ''),
    ('skip-v3-failed', 'skip-scope', 'gen', 'reducer', 'container_image_identity', 'failed', 3,
     '2026-09-18T00:00:00Z', now(), now(), 7, false, '', true, 'failed'),
    ('skip-plain-pending', 'skip-scope', 'gen', 'reducer', 'container_image_identity', 'pending', 3,
     '2026-09-18T00:00:00Z', now(), now(), 7, false, '', false, ''),
    ('skip-claimed', 'skip-scope', 'gen', 'reducer', 'container_image_identity', 'claimed', 3,
     '2026-09-18T00:00:00Z', now(), now(), 7, true, 'claimed', true, 'claimed'),
    ('skip-running', 'skip-scope', 'gen', 'reducer', 'container_image_identity', 'running', 3,
     '2026-09-18T00:00:00Z', now(), now(), 7, true, 'running', true, 'running'),
    ('skip-succeeded', 'skip-scope', 'gen', 'reducer', 'container_image_identity', 'succeeded', 3,
     '2026-09-18T00:00:00Z', now(), now(), 7, true, 'succeeded', true, 'succeeded'),
    ('skip-superseded', 'skip-scope', 'gen', 'reducer', 'container_image_identity', 'superseded', 3,
     '2026-09-18T00:00:00Z', now(), now(), 7, true, 'superseded', true, 'superseded');
UPDATE fact_work_items SET lease_owner='worker', claim_until=now()+interval '10 minutes'
WHERE work_item_id IN ('skip-claimed', 'skip-running');`); err != nil {
		t.Fatalf("seed skip work items: %v", err)
	}
	items, err = adminStore.SkipRepositoryWorkItems(ctx, "skip-repo", "operator skip")
	if err != nil || len(items) != 4 {
		t.Fatalf("skip eligible repository work = %d items, %v; want four", len(items), err)
	}
	for _, test := range []struct{ id, status, v2, v3 string }{
		{"skip-both-pending", "dead_letter", "dead_letter", "dead_letter"},
		{"skip-v2-retrying", "dead_letter", "dead_letter", ""},
		{"skip-v3-failed", "dead_letter", "", "dead_letter"},
		{"skip-plain-pending", "dead_letter", "", ""},
		{"skip-claimed", "claimed", "claimed", "claimed"},
		{"skip-running", "running", "running", "running"},
		{"skip-succeeded", "succeeded", "succeeded", "succeeded"},
		{"skip-superseded", "superseded", "superseded", "superseded"},
	} {
		checkIdentityWorkItem(t, ctx, db, test.id, test.status, test.v2, test.v3)
	}
	items, err = adminStore.SkipRepositoryWorkItems(ctx, "skip-repo", "repeat skip")
	if err != nil || len(items) != 0 {
		t.Fatalf("repeat skip = %d items, %v; want empty", len(items), err)
	}
	for _, id := range []string{"skip-claimed", "skip-running"} {
		var owner string
		if err := db.QueryRowContext(ctx, "SELECT lease_owner FROM fact_work_items WHERE work_item_id=$1", id).Scan(&owner); err != nil || owner != "worker" {
			t.Fatalf("%s lease owner = %q, %v; want worker", id, owner, err)
		}
	}

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

// TestAdminStoreMutationsRecheckStatusLive proves an admin mutation cannot
// overwrite a worker claim or a newer eligible status after selection.
func TestAdminStoreMutationsRecheckStatusLive(t *testing.T) {
	for _, mode := range []string{"replay", "dead-letter", "skip", "skip-retrying"} {
		t.Run(mode, func(t *testing.T) { runAdminStoreStatusRecheckLive(t, mode) })
	}
}

func runAdminStoreStatusRecheckLive(t *testing.T, mode string) {
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
CREATE TABLE `+schema+`.ingestion_scopes (scope_id text PRIMARY KEY, source_key text NOT NULL);
INSERT INTO `+schema+`.ingestion_scopes VALUES ('scope', 'repo');
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
	if mode == "skip" || mode == "skip-retrying" {
		if _, err := adminDB.ExecContext(ctx, "UPDATE "+schema+".fact_work_items SET status='pending', container_image_identity_v2_authorized_status='pending', container_image_identity_v3_authorized_status='pending' WHERE work_item_id='race'"); err != nil {
			t.Fatal(err)
		}
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
	workerTransition := `UPDATE fact_work_items
SET status='claimed', container_image_identity_v2_authorized_status='claimed',
    container_image_identity_v3_authorized_status='claimed',
    lease_owner='worker', claim_until=now()+interval '10 minutes'
WHERE work_item_id='race'`
	if mode == "skip-retrying" {
		workerTransition = `UPDATE fact_work_items
SET status='retrying', container_image_identity_v2_authorized_status='retrying',
    container_image_identity_v3_authorized_status='retrying'
WHERE work_item_id='race'`
	}
	if _, err := claimTx.ExecContext(ctx, workerTransition); err != nil {
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
		switch mode {
		case "dead-letter":
			items, mutationErr = adminStore.DeadLetterWorkItems(ctx, admin.DeadLetterFilter{
				WorkItemIDs: []string{"race"}, Stage: "reducer", FailureClass: "projection_bug", Limit: 1,
			})
		case "skip", "skip-retrying":
			items, mutationErr = adminStore.SkipRepositoryWorkItems(ctx, "repo", "operator skip")
		default:
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
		select {
		case result := <-finished:
			t.Fatalf("admin mutation finished before competing claim committed: %d items, %v", result.count, result.err)
		default:
			t.Fatal("admin mutation never waited for the competing claim row lock")
		}
	}
	if err := claimTx.Commit(); err != nil {
		t.Fatal(err)
	}
	result := <-finished
	if result.err != nil || result.count != 0 {
		t.Fatalf("admin mutation after competing claim = %d items, %v; want no mutation", result.count, result.err)
	}
	var status string
	var owner sql.NullString
	if err := claimDB.QueryRowContext(ctx, "SELECT status,lease_owner FROM fact_work_items WHERE work_item_id='race'").Scan(&status, &owner); err != nil {
		t.Fatal(err)
	}
	wantStatus := "claimed"
	wantOwner := "worker"
	if mode == "skip-retrying" {
		wantStatus, wantOwner = "retrying", ""
	}
	if status != wantStatus || owner.String != wantOwner {
		t.Fatalf("competing status change was overwritten: status=%s owner=%s; want %s/%s", status, owner.String, wantStatus, wantOwner)
	}
}
