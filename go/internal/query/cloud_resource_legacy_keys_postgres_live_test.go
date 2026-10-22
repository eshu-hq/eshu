// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #751 disposable PostgreSQL proof that historical AppSync API-key rows are suppressed while healthy rows fill the requested page; one local PostgreSQL 18 run completed in 1.083s.
//eshu:readiness:env ESHU_APPSYNC_LEGACY_KEYS_PROOF_DISPOSABLE ESHU_APPSYNC_LEGACY_KEYS_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCloudResourceListSuppressesHistoricalAppSyncKeysLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_APPSYNC_LEGACY_KEYS_PROOF_DISPOSABLE")) != "1" {
		t.Skip("set the AppSync legacy-key disposable proof opt-in")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_APPSYNC_LEGACY_KEYS_PROOF_DSN"))
	if dsn == "" {
		t.Fatal("ESHU_APPSYNC_LEGACY_KEYS_PROOF_DSN is required when disposable proof is enabled")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedCloudResourceListLiveCorpus(t, ctx, db)
	if _, err := db.ExecContext(ctx, `UPDATE graph_node_owner SET winning_row = jsonb_set(winning_row, '{data_type}', to_jsonb('aws_appsync_api_key'::text)) WHERE uid = 'uid-000001'`); err != nil {
		t.Fatalf("seed historical key owner row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records SET payload = jsonb_set(payload, '{data_type}', to_jsonb('aws_appsync_api_key'::text)) WHERE fact_id = 'fact-000001'`); err != nil {
		t.Fatalf("seed historical key fact: %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE graph_node_owner SET winning_row = jsonb_set(winning_row, '{data_type}', to_jsonb('aws_appsync_api_key'::text)) WHERE uid = 'uid-000002'`); err != nil {
		t.Fatalf("seed conflicting historical key owner row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records SET payload = jsonb_set(payload, '{data_type}', to_jsonb('aws_appsync_api_key'::text)) WHERE fact_id = 'fact-000002'`); err != nil {
		t.Fatalf("seed conflicting historical key fact: %v", err)
	}

	rows, err := NewPostgresCloudResourceListStore(db).ListCloudResourceIdentities(ctx, CloudResourceListPageFilter{Limit: 2, AllScopes: true})
	if err != nil {
		t.Fatalf("list cloud-resource identities: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("list returned %d rows, want a full page of 2 healthy resources", len(rows))
	}
	for _, row := range rows {
		if row.UID == "uid-000001" || row.UID == "uid-000002" || row.ResourceType == "aws_appsync_api_key" {
			t.Fatalf("historical key leaked into cloud-resource page: %#v", row)
		}
	}
}
