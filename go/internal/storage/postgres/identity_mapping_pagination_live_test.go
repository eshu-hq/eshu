// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestAdminMappingListPaginationLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the mapping pagination proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin isolated fixture: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
CREATE TEMP TABLE identity_provider_group_role_mappings (
    provider_config_id text NOT NULL,
    tenant_id text NOT NULL,
    workspace_id text NOT NULL,
    role_id text NOT NULL,
    external_group_hash text NOT NULL,
    status text NOT NULL,
    effective_at timestamptz NOT NULL,
    expires_at timestamptz,
    tombstoned_at timestamptz
) ON COMMIT DROP;
SET LOCAL search_path = pg_temp;
INSERT INTO identity_provider_group_role_mappings
SELECT 'provider', 'tenant', 'workspace', 'role', n::text,
       'active', now(), NULL, NULL
FROM generate_series(1, 501) AS n;
INSERT INTO identity_provider_group_role_mappings
VALUES ('provider', 'other', 'workspace', 'role', 'secret', 'active', now(), NULL, NULL),
       ('provider', 'tenant', 'workspace', 'role', 'deleted', 'active', now(), NULL, now());
`); err != nil {
		t.Fatalf("create mapping fixture: %v", err)
	}
	cursor := ""
	seen := map[string]bool{}
	pageSizes := []int{}
	for {
		rows, err := tx.QueryContext(ctx, listAdminIdPGroupMappingsQuery, "tenant", "workspace", cursor)
		if err != nil {
			t.Fatalf("query page: %v", err)
		}
		pageSize := 0
		for rows.Next() {
			var ref, provider, role, status, tenant, workspace string
			var effectiveAt time.Time
			var expiresAt sql.NullTime
			if err := rows.Scan(&ref, &provider, &role, &status, &effectiveAt,
				&expiresAt, &tenant, &workspace); err != nil {
				t.Fatalf("scan page: %v", err)
			}
			if len(ref) != 64 || ref <= cursor || seen[ref] || tenant != "tenant" || workspace != "workspace" {
				t.Fatalf("invalid or repeated scoped ref %q after %q", ref, cursor)
			}
			seen[ref] = true
			cursor = ref
			pageSize++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read page: %v", err)
		}
		_ = rows.Close()
		pageSizes = append(pageSizes, pageSize)
		if pageSize < 500 {
			break
		}
	}
	if len(seen) != 501 || len(pageSizes) != 2 || pageSizes[0] != 500 || pageSizes[1] != 1 {
		t.Fatalf("pages=%v unique refs=%d, want [500 1] and 501", pageSizes, len(seen))
	}
}
