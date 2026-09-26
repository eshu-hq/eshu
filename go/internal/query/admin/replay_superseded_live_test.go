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

// TestAdminStoreSupersededGenerationReplayFenceLive runs the #7130 admin
// replay fence and its explicit-id read against Postgres. Temporary tables
// keep this proof off durable data.
func TestAdminStoreSupersededGenerationReplayFenceLive(t *testing.T) {
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
CREATE TEMP TABLE scope_generations (generation_id text PRIMARY KEY, status text NOT NULL);
CREATE TEMP TABLE fact_work_items (
    work_item_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
    stage text NOT NULL, domain text NOT NULL, status text NOT NULL,
    attempt_count integer NOT NULL, lease_owner text, claim_until timestamptz,
    visible_at timestamptz, next_attempt_at timestamptz, last_attempt_at timestamptz,
    failure_class text, failure_message text, failure_details text,
    created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
    container_image_identity_v2_required boolean NOT NULL DEFAULT false,
    container_image_identity_v2_authorized_status text NOT NULL DEFAULT '',
    container_image_identity_v3_required boolean NOT NULL DEFAULT false,
    container_image_identity_v3_authorized_status text NOT NULL DEFAULT ''
);
CREATE TEMP TABLE fact_replay_events (
    replay_event_id text PRIMARY KEY, work_item_id text NOT NULL,
    scope_id text NOT NULL, generation_id text NOT NULL,
    failure_class text, operator_note text, created_at timestamptz NOT NULL
);
SET search_path TO pg_temp;
INSERT INTO scope_generations VALUES ('gen-old', 'superseded'), ('gen-new', 'active');
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    failure_class, failure_message, created_at, updated_at
) VALUES
    ('projector-old', 'scope-a', 'gen-old', 'projector', 'source_local', 'dead_letter', 3,
     'retry_exhausted', 'old', now(), now()),
    ('projector-new', 'scope-a', 'gen-new', 'projector', 'source_local', 'dead_letter', 3,
     'retry_exhausted', 'new', now(), now()),
    ('reducer-old', 'scope-a', 'gen-old', 'reducer', 'workload_identity', 'dead_letter', 3,
     'retry_exhausted', 'old', now(), now());
`); err != nil {
		t.Fatalf("seed temporary superseded-generation work items: %v", err)
	}

	adminStore := store.NewStore(db)
	ids := []string{"projector-old", "projector-new", "reducer-old", "missing"}
	targets, err := adminStore.SupersededReplayTargets(ctx, admin.UnsafeReplayTargetFilter{WorkItemIDs: ids})
	if err != nil {
		t.Fatalf("read superseded replay targets: %v", err)
	}
	if len(targets) != 1 || targets[0].WorkItemID != "projector-old" || targets[0].GenerationID != "gen-old" {
		t.Fatalf("superseded targets = %+v, want only projector-old on gen-old", targets)
	}
	if reducerOnly, err := adminStore.SupersededReplayTargets(ctx, admin.UnsafeReplayTargetFilter{
		WorkItemIDs: ids, Stage: "reducer",
	}); err != nil || len(reducerOnly) != 0 {
		t.Fatalf("reducer-stage superseded read = %+v, %v; want none (fence is projector-only)", reducerOnly, err)
	}

	// A broad replay skips the superseded-generation projector row and keeps
	// replaying reducer work on the same generation.
	items, err := adminStore.ReplayFailedWorkItems(ctx, admin.ReplayWorkItemFilter{
		OperatorNote: "retry", Limit: 10,
	})
	if err != nil {
		t.Fatalf("broad replay: %v", err)
	}
	replayed := map[string]bool{}
	for _, item := range items {
		replayed[item.WorkItemID] = true
	}
	if len(items) != 2 || !replayed["projector-new"] || !replayed["reducer-old"] {
		t.Fatalf("replayed %+v, want projector-new and reducer-old only", items)
	}
	var status string
	if err := db.QueryRowContext(ctx,
		"SELECT status FROM fact_work_items WHERE work_item_id = 'projector-old'").Scan(&status); err != nil {
		t.Fatalf("read projector-old status: %v", err)
	}
	if status != "dead_letter" {
		t.Fatalf("projector-old status = %s, want dead_letter (fenced)", status)
	}
}
