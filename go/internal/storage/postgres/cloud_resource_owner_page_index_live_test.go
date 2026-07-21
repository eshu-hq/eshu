// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCloudResourceOwnerPageIndexesApplyAndReapplyLive(t *testing.T) {
	const schema = "eshu_5563_cloud_resource_page_live"

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live cloud resource index proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop isolated proof schema: %v", err)
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	parsedDSN.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, `
CREATE TABLE graph_node_owner (
  uid text PRIMARY KEY,
  source_order_key text NOT NULL,
  winning_row jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT NOW()
);
INSERT INTO graph_node_owner (uid, source_order_key, winning_row)
SELECT 'uid-' || lpad(value::text, 6, '0'),
       lpad(value::text, 6, '0'),
       jsonb_build_object(
         'resource_type', 'type-' || lpad((value % 20)::text, 2, '0'),
         'collector_kind', 'provider-' || lpad((value % 4)::text, 2, '0'),
         'region', 'region-' || lpad((value % 8)::text, 2, '0'),
         'account_id', 'account-' || lpad((value % 16)::text, 2, '0')
       )
FROM generate_series(1, 20000) AS value;
`); err != nil {
		t.Fatalf("seed populated graph owner ledger: %v", err)
	}

	definitions := cloudResourceOwnerPageIndexDefinitions(t)
	for pass := 1; pass <= 2; pass++ {
		if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
			t.Fatalf("apply cloud resource page indexes pass %d: %v", pass, err)
		}
	}

	rows, err := db.QueryContext(ctx, `
SELECT c.relname, i.indisvalid, i.indisready
FROM pg_index AS i
JOIN pg_class AS c ON c.oid = i.indexrelid
JOIN pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname = $1
  AND c.relname = ANY($2::text[])
ORDER BY c.relname
`, schema, []string{
		"graph_node_owner_cloud_resource_account_page_idx",
		"graph_node_owner_cloud_resource_page_idx",
		"graph_node_owner_cloud_resource_provider_page_idx",
		"graph_node_owner_cloud_resource_region_page_idx",
	})
	if err != nil {
		t.Fatalf("inspect cloud resource page indexes: %v", err)
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		var name string
		var valid, ready bool
		if err := rows.Scan(&name, &valid, &ready); err != nil {
			t.Fatalf("scan index state: %v", err)
		}
		if !valid || !ready {
			t.Errorf("index %s state valid=%t ready=%t, want true/true", name, valid, ready)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate index state: %v", err)
	}
	if count != 4 {
		t.Fatalf("cloud resource page indexes = %d, want 4", count)
	}
}

func cloudResourceOwnerPageIndexDefinitions(t *testing.T) []Definition {
	t.Helper()
	wanted := map[string]struct{}{
		"cloud_resource_owner_page_index":          {},
		"cloud_resource_owner_provider_page_index": {},
		"cloud_resource_owner_region_page_index":   {},
		"cloud_resource_owner_account_page_index":  {},
	}
	definitions := make([]Definition, 0, len(wanted))
	for _, definition := range BootstrapDefinitions() {
		if _, ok := wanted[definition.Name]; ok {
			definitions = append(definitions, definition)
		}
	}
	if len(definitions) != len(wanted) {
		t.Fatalf("cloud resource page definitions = %d, want %d", len(definitions), len(wanted))
	}
	return definitions
}
