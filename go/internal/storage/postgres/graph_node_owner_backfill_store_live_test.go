// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestLiveGraphNodeOwnerBackfillPreservesRealOwnersAndScales(t *testing.T) {
	if os.Getenv("ESHU_GRAPH_NODE_OWNER_BACKFILL_LIVE") != "1" {
		t.Skip("set ESHU_GRAPH_NODE_OWNER_BACKFILL_LIVE=1 to run the live Postgres proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Fatal("ESHU_POSTGRES_DSN is required")
	}

	ctx := t.Context()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	schema := fmt.Sprintf("eshu_owner_backfill_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("set isolated search path: %v", err)
	}
	for _, definition := range []string{"graph_node_owner", "graph_node_owner_backfill_state"} {
		if _, err := db.ExecContext(ctx, MigrationSQL(definition)); err != nil {
			t.Fatalf("apply %s migration: %v", definition, err)
		}
	}

	realKey := "2026-07-21T18:00:00.000000000Z|fact-real"
	realRow := `{"uid":"uid-race","resource_type":"aws_s3_bucket","source_fact_id":"fact-real"}`
	if _, err := db.ExecContext(ctx, `
INSERT INTO graph_node_owner (uid, source_order_key, winning_row, updated_at)
VALUES ($1, $2, $3, now())`, "uid-race", realKey, []byte(realRow)); err != nil {
		t.Fatalf("seed real reducer owner: %v", err)
	}

	const rowCount = 20000
	entries := make([]GraphNodeOwnerEntry, 0, rowCount)
	for index := 0; index < rowCount; index++ {
		uid := fmt.Sprintf("uid-%05d", index)
		factID := fmt.Sprintf("fact-%05d", index)
		row, err := json.Marshal(map[string]any{
			"uid":            uid,
			"resource_type":  "aws_s3_bucket",
			"source_fact_id": factID,
		})
		if err != nil {
			t.Fatalf("marshal row %d: %v", index, err)
		}
		entries = append(entries, GraphNodeOwnerEntry{
			UID:            uid,
			SourceOrderKey: GraphNodeOwnerBackfillMinimumOrderKeyPrefix + factID,
			WinningRow:     row,
		})
	}
	entries = append(entries, GraphNodeOwnerEntry{
		UID:            "uid-race",
		SourceOrderKey: GraphNodeOwnerBackfillMinimumOrderKeyPrefix + "fact-old",
		WinningRow:     json.RawMessage(`{"uid":"uid-race","resource_type":"aws_s3_bucket","source_fact_id":"fact-old"}`),
	})

	store := NewGraphNodeOwnerBackfillStore(SQLDB{DB: db})
	started := time.Now()
	if err := store.SeedExistingGraphNodeOwners(ctx, entries, started.UTC()); err != nil {
		t.Fatalf("seed %d existing owners: %v", rowCount, err)
	}
	duration := time.Since(started)
	t.Logf("seeded %d existing graph owners in %s", rowCount, duration)

	var gotKey, gotFactID string
	if err := db.QueryRowContext(ctx, `
SELECT source_order_key, winning_row->>'source_fact_id'
FROM graph_node_owner
WHERE uid = 'uid-race'`).Scan(&gotKey, &gotFactID); err != nil {
		t.Fatalf("read race owner: %v", err)
	}
	if gotKey != realKey || gotFactID != "fact-real" {
		t.Fatalf("minimum-key backfill displaced real owner: key=%q fact=%q", gotKey, gotFactID)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM graph_node_owner").Scan(&count); err != nil {
		t.Fatalf("count seeded owners: %v", err)
	}
	if got, want := count, rowCount+1; got != want {
		t.Fatalf("owner count = %d, want %d", got, want)
	}
	if err := store.MarkCloudResourceBackfillComplete(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("mark completion: %v", err)
	}
	complete, err := store.IsCloudResourceBackfillComplete(ctx)
	if err != nil {
		t.Fatalf("read completion: %v", err)
	}
	if !complete {
		t.Fatal("completion marker = false, want true")
	}
}
