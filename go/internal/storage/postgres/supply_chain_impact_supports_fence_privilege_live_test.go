// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const supportsFenceFunction = "supply_chain_impact_finding_supports_mark_dirty"

var supportsFenceRoleSeq atomic.Int64

// supportsFenceRestrictedConn returns a pinned connection running as a
// login-like role that may read, insert and update fact_records and nothing
// else: no privilege on the supports dirty table, the way a least-privilege
// reducer login that was never granted it looks. A control checks each of the
// dirty table's privileges separately, so a grant that crept into the fixture
// fails the test instead of hiding the defect.
func supportsFenceRestrictedConn(t *testing.T, ctx context.Context, db *sql.DB) *sql.Conn {
	t.Helper()
	role := fmt.Sprintf("supports_fence_proof_%d_%d", time.Now().UnixNano(), supportsFenceRoleSeq.Add(1))
	for _, statement := range []string{
		`CREATE ROLE ` + role + ` NOLOGIN`,
		`GRANT USAGE ON SCHEMA public TO ` + role,
		`GRANT SELECT, INSERT, UPDATE ON fact_records TO ` + role,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP OWNED BY `+role)
		_, _ = db.ExecContext(context.Background(), `DROP ROLE IF EXISTS `+role)
	})
	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		var held bool
		if err := db.QueryRowContext(ctx, `SELECT has_table_privilege($1, 'supply_chain_impact_finding_supports_dirty', $2)`, role, privilege).Scan(&held); err != nil {
			t.Fatalf("check %s on the dirty table: %v", privilege, err)
		}
		if held {
			t.Fatalf("the proof role holds %s on the dirty table; the fixture must leave it with none", privilege)
		}
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin a connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `RESET ROLE`)
		_ = conn.Close()
	})
	if _, err := conn.ExecContext(ctx, `SET ROLE `+role); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

// supportsFenceInsertFinding inserts one finding fact on conn without the
// maintainer's transaction setting, the way a reducer of the previous release
// does. It returns the insert error so a test can assert on it.
func supportsFenceInsertFinding(ctx context.Context, conn *sql.Conn, factID, kind string) error {
	_, err := conn.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
    observed_at, ingested_at, is_tombstone, payload, fencing_token)
VALUES ($1, $2, $3, $4, $1, 'synthetic', $1, now(), now(), FALSE,
    '{"cve_id":"CVE-2026-0001","package_id":"pkg:npm/old@1","repository_id":"repository:old","impact_status":"affected_exact"}'::jsonb, 1)`,
		factID, replaceSetLiveScope, replaceSetLiveGeneration, kind)
	return err
}

// TestSupplyChainImpactSupportsFenceRunsWithoutDirtyTablePrivilegesLive proves
// the old-writer fence trigger runs as its owner. A login with INSERT and UPDATE
// on fact_records and no privilege on the dirty table inserts a finding fact
// without the writer setting and then tombstones it; both succeed and the scope
// is marked, with its version bumped by the second write. Run as the caller, the
// trigger fails with permission denied (SQLSTATE 42501) on the dirty table,
// which is what a previous-release reducer on a least-privilege database would
// hit from the migration's commit until a DBA granted by hand.
func TestSupplyChainImpactSupportsFenceRunsWithoutDirtyTablePrivilegesLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	conn := supportsFenceRestrictedConn(t, ctx, db)

	if err := supportsFenceInsertFinding(ctx, conn, "fact:fence-priv:insert", "reducer_supply_chain_impact_finding"); err != nil {
		t.Fatalf("old-writer insert as a login with no dirty-table privilege: %v", err)
	}
	version, found := supportsDirtyVersion(t, ctx, db)
	if !found || version != 1 {
		t.Fatalf("dirty mark after the insert = version %d, found %v; want version 1", version, found)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'fact:fence-priv:insert'`); err != nil {
		t.Fatalf("old-writer tombstone update as a login with no dirty-table privilege: %v", err)
	}
	if version, found := supportsDirtyVersion(t, ctx, db); !found || version != 2 {
		t.Fatalf("dirty mark after the update = version %d, found %v; want version 2", version, found)
	}
}

// TestSupplyChainImpactSupportsFenceSkipsTheWriterAndOtherKindsLive proves the
// fence still skips what it must once it runs as its owner: the trigger's WHEN
// clause reads the caller's transaction setting before the function is entered,
// so the setting skips the mark for a restricted login, and a fact of another
// kind never marks. Neither write touches the dirty table, so neither needs a
// privilege on it.
func TestSupplyChainImpactSupportsFenceSkipsTheWriterAndOtherKindsLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	conn := supportsFenceRestrictedConn(t, ctx, db)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, supportsFenceWriterSetting); err != nil {
		t.Fatalf("set the writer setting: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
    observed_at, ingested_at, is_tombstone, payload, fencing_token)
VALUES ('fact:fence-priv:writer', $1, $2, 'reducer_supply_chain_impact_finding', 'fact:fence-priv:writer', 'synthetic',
    'fact:fence-priv:writer', now(), now(), FALSE, '{}'::jsonb, 1)`, replaceSetLiveScope, replaceSetLiveGeneration); err != nil {
		t.Fatalf("insert with the writer setting: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if n := supportsDirtyScopes(t, ctx, db); n != 0 {
		t.Fatalf("a write that carries the writer setting marked %d scopes dirty, want 0", n)
	}
	if err := supportsFenceInsertFinding(ctx, conn, "fact:fence-priv:other-kind", "vulnerability.cve"); err != nil {
		t.Fatalf("insert of another fact kind: %v", err)
	}
	if n := supportsDirtyScopes(t, ctx, db); n != 0 {
		t.Fatalf("a fact of another kind marked %d scopes dirty, want 0", n)
	}
}

// TestSupplyChainImpactSupportsFenceIgnoresCallerTempTablesLive is the pg_temp
// regression for the fence function. A caller that may create temporary tables
// can shadow an unqualified relation name, because Postgres searches the
// session temp schema first unless pg_temp is last in the search_path. The
// caller creates a temporary table named like the dirty table; the mark must
// still land in the real table, not the caller's.
func TestSupplyChainImpactSupportsFenceIgnoresCallerTempTablesLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	conn := supportsFenceRestrictedConn(t, ctx, db)

	if _, err := conn.ExecContext(ctx, `CREATE TEMP TABLE supply_chain_impact_finding_supports_dirty
(scope_id text PRIMARY KEY, dirty_version bigint NOT NULL DEFAULT 1, marked_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatalf("shadow the dirty table: %v", err)
	}
	if err := supportsFenceInsertFinding(ctx, conn, "fact:fence-priv:shadow", "reducer_supply_chain_impact_finding"); err != nil {
		t.Fatalf("old-writer insert with a shadowed dirty table: %v", err)
	}
	if version, found := supportsDirtyVersion(t, ctx, db); !found || version != 1 {
		var shadowed int
		_ = conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_temp.supply_chain_impact_finding_supports_dirty`).Scan(&shadowed)
		t.Fatalf("real dirty table = version %d, found %v; want the mark (the caller's temp table caught %d rows)", version, found, shadowed)
	}
}

// TestSupplyChainImpactSupportsFenceFunctionPostureLive pins the definer posture
// of the fence function in a production-shaped public schema and in an isolated
// schema: SECURITY DEFINER, PUBLIC execute revoked, and a search_path of
// pg_catalog, that one schema and pg_temp last. In the isolated schema the mark
// must land in the function's own schema, not public.
func TestSupplyChainImpactSupportsFenceFunctionPostureLive(t *testing.T) {
	t.Run("public", func(t *testing.T) {
		ctx, db := openReplaceSetLiveDB(t)
		assertSupportsFenceFunction(ctx, t, db, "public")
	})
	t.Run("isolated schema", func(t *testing.T) {
		adminDSN, optIn := supplyChainImpactDisposableAdmin(t, "#10880 fence function posture proof")
		template := postgresproof.OpenIsolatedBootstrapTemplate(t, adminDSN, optIn, 5*time.Minute,
			func(ctx context.Context, database *sql.DB) error {
				return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
			})
		_, database := postgresproof.OpenIsolatedBootstrapTemplateClone(t, adminDSN, optIn, 5*time.Minute, template)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		t.Cleanup(cancel)
		var schema string
		if err := database.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil || schema == "public" {
			t.Fatalf("current_schema = %q, %v; want an isolated schema", schema, err)
		}
		assertSupportsFenceFunction(ctx, t, database, schema)

		for _, statement := range []string{
			`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload)
VALUES ('fence-iso-scope', 'vulnerability_intelligence', 'synthetic', 'fence-iso-scope', 'synthetic', 'fence-iso-scope', now(), now(), 'active', '{}'::jsonb)`,
			`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('fence-iso-g1', 'fence-iso-scope', 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`,
			`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('fence-iso-f1', 'fence-iso-scope', 'fence-iso-g1', 'reducer_supply_chain_impact_finding', 'fence-iso-f1', '1.0.0',
  'synthetic', 'fence-iso-f1', now(), now(), false, '{}'::jsonb)`,
		} {
			if _, err := database.ExecContext(ctx, statement); err != nil {
				t.Fatalf("seed the isolated schema: %v\n%s", err, statement)
			}
		}
		var marks int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports_dirty WHERE scope_id = 'fence-iso-scope'`).Scan(&marks); err != nil || marks != 1 {
			t.Fatalf("isolated schema %s holds %d marks for the old-writer insert (err %v), want 1 in the function's own schema", schema, marks, err)
		}
	})
}

// assertSupportsFenceFunction requires exactly one fence function in schema,
// with the definer posture.
func assertSupportsFenceFunction(ctx context.Context, t *testing.T, database *sql.DB, schema string) {
	t.Helper()
	rows, err := database.QueryContext(ctx, `SELECT p.prosecdef, array_to_string(p.proconfig, ';'), p.proacl IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM aclexplode(p.proacl) AS a WHERE a.grantee = 0)
FROM pg_proc AS p JOIN pg_namespace AS n ON n.oid = p.pronamespace
WHERE n.nspname = $1 AND p.proname = $2`, schema, supportsFenceFunction)
	if err != nil {
		t.Fatalf("list the fence function: %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var definer, publicRevoked bool
		var config sql.NullString
		if err := rows.Scan(&definer, &config, &publicRevoked); err != nil {
			t.Fatalf("scan the fence function: %v", err)
		}
		seen++
		if !definer || !publicRevoked {
			t.Errorf("%s.%s: security definer = %t, PUBLIC execute revoked = %t; want both true", schema, supportsFenceFunction, definer, publicRevoked)
		}
		if want := "search_path=pg_catalog, " + schema + ", pg_temp"; !config.Valid || !strings.EqualFold(config.String, want) {
			t.Errorf("%s.%s: proconfig = %q, want %q", schema, supportsFenceFunction, config.String, want)
		}
	}
	if seen != 1 {
		t.Fatalf("schema %s holds %d fence functions, want 1", schema, seen)
	}
}
