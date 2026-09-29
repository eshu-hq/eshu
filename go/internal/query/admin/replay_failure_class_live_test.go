// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestAdminStoreReplayRecordsPriorFailureClassLive proves #7387: replaying a
// dead-lettered row must record the class it had failed with in
// fact_replay_events, even though the replay itself clears failure_class on
// the work row. Temporary tables keep this proof off durable data.
func TestAdminStoreReplayRecordsPriorFailureClassLive(t *testing.T) {
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
-- The replay's #7130 superseded-generation fence reads scope_generations.
CREATE TEMP TABLE scope_generations (generation_id text PRIMARY KEY, status text NOT NULL);
CREATE TEMP TABLE fact_replay_events (
    replay_event_id text PRIMARY KEY, work_item_id text NOT NULL,
    scope_id text NOT NULL, generation_id text NOT NULL,
    failure_class text, operator_note text, created_at timestamptz NOT NULL
);
SET search_path TO pg_temp;
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    last_attempt_at, failure_class, failure_message, created_at, updated_at,
    container_image_identity_claim_epoch,
    container_image_identity_v2_required, container_image_identity_v2_authorized_status,
    container_image_identity_v3_required, container_image_identity_v3_authorized_status
) VALUES
    ('replay-class', 'replay-scope', 'gen', 'reducer', 'container_image_identity', 'dead_letter', 3,
     '2026-09-18T00:00:00Z', 'projection_bug', 'old FIPS failure', now(), now(), 7, false, '', false, '');
`); err != nil {
		t.Fatalf("seed temporary replay work item: %v", err)
	}

	adminStore := store.NewStore(db)
	items, err := adminStore.ReplayFailedWorkItems(ctx, admin.ReplayWorkItemFilter{
		WorkItemIDs: []string{"replay-class"}, Stage: "reducer", OperatorNote: "retry", Limit: 1,
	})
	if err != nil {
		t.Fatalf("replay dead-lettered row: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("replayed %d items, want one", len(items))
	}
	// The replayed item itself carries post-update truth: pending with no
	// failure class, matching the work row the replay wrote.
	if items[0].Status != "pending" || items[0].FailureClass != nil {
		t.Fatalf("replayed item = status %q class %+v, want pending with nil class",
			items[0].Status, items[0].FailureClass)
	}
	var rowClass sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT failure_class FROM fact_work_items WHERE work_item_id='replay-class'").Scan(&rowClass); err != nil {
		t.Fatal(err)
	}
	if rowClass.Valid {
		t.Fatalf("work row failure_class = %q, want NULL after replay", rowClass.String)
	}

	var eventClass sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT failure_class FROM fact_replay_events WHERE work_item_id='replay-class'").Scan(&eventClass); err != nil {
		t.Fatalf("read replay event: %v", err)
	}
	if !eventClass.Valid || eventClass.String != "projection_bug" {
		t.Fatalf("replay event failure_class = %+v, want projection_bug", eventClass)
	}
}
