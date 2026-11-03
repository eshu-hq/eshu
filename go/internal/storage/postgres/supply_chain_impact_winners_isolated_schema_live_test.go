// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSupplyChainImpactWinnersResweepCaptureTriggersBindTheirOwnSchemaLive is the
// isolated-schema regression for migration 272 (#9805 review N1). An isolated
// schema bootstrap (the proof surface of many existing live tests) skips the
// public-only scanner migrations and creates every table in its own schema, so
// the pointer and delete trigger functions must find the dirty log and
// fact_records in the schema they were created in, not in public. The test
// bootstraps a fresh schema, moves a pointer, deletes a scope that holds a
// finding, and requires the log rows in that schema. It also pins the definer
// posture: SECURITY DEFINER, a search_path of pg_catalog, that one schema and pg_temp last,
// and no PUBLIC execute.
func TestSupplyChainImpactWinnersResweepCaptureTriggersBindTheirOwnSchemaLive(t *testing.T) {
	dsn := supplyChainImpactLiveDSN(t, "#9805 isolated schema capture proof")
	database := postgresproof.OpenIsolatedSchema(t, dsn, "winners_isolated", func(ctx context.Context, database *sql.DB) error {
		return postgres.ApplyBootstrapWithoutContentSearchIndexes(ctx, postgres.SQLDB{DB: database})
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec: %v\n%s", err, query)
		}
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := database.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("count: %v\n%s", err, query)
		}
		return n
	}
	var schema string
	if err := database.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil || schema == "public" {
		t.Fatalf("current_schema = %q, %v; want an isolated schema", schema, err)
	}

	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload, active_generation_id)
VALUES ('iso-scope', 'vulnerability_intelligence', 'synthetic', 'iso-scope', 'synthetic', 'iso-scope', now(), now(), 'active', '{}'::jsonb, NULL)`)
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('iso-g1', 'iso-scope', 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`)
	exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('iso-f1', 'iso-scope', 'iso-g1', $1, 'iso-f1', '1.0.0', 'synthetic', 'iso-f1', now(), now(), false,
  '{"cve_id":"CVE-2026-9805","package_id":"pkg:npm/iso","repository_id":"repo-iso","subject_digest":"","advisory_id":""}'::jsonb)`,
		facts.ReducerSupplyChainImpactFindingFactKind)

	exec(`UPDATE ingestion_scopes SET active_generation_id = 'iso-g1' WHERE scope_id = 'iso-scope'`)
	if got := count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope' AND scope_id = 'iso-scope'`); got != 1 {
		t.Fatalf("pointer move wrote %d scope rows in schema %s, want 1", got, schema)
	}
	exec(`DELETE FROM ingestion_scopes WHERE scope_id = 'iso-scope'`)
	if got := count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact' AND package_id = 'pkg:npm/iso'`); got != 1 {
		t.Fatalf("scope delete wrote %d fact rows in schema %s, want 1", got, schema)
	}

	assertWinnersCaptureFunctions(ctx, t, database, schema, 2)
}

// assertWinnersCaptureFunctions pins the definer posture of the capture
// functions in schema: SECURITY DEFINER, PUBLIC execute revoked, and a
// search_path of pg_catalog, that one schema (public in a production
// bootstrap; scanner functions use the public-only convention) and pg_temp
// last. It requires exactly want functions there.
func assertWinnersCaptureFunctions(ctx context.Context, t *testing.T, database *sql.DB, schema string, want int) {
	t.Helper()
	rows, err := database.QueryContext(ctx, `SELECT p.proname, p.prosecdef, array_to_string(p.proconfig, ';'), p.proacl IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM aclexplode(p.proacl) AS a WHERE a.grantee = 0)
FROM pg_proc AS p JOIN pg_namespace AS n ON n.oid = p.pronamespace
WHERE n.nspname = $1 AND p.proname LIKE 'supply_chain_impact_winners_capture_%'`, schema)
	if err != nil {
		t.Fatalf("list capture functions: %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var name string
		var definer, publicRevoked bool
		var config sql.NullString
		if err := rows.Scan(&name, &definer, &config, &publicRevoked); err != nil {
			t.Fatalf("scan function: %v", err)
		}
		seen++
		if !definer || !publicRevoked {
			t.Fatalf("%s: security definer = %t, PUBLIC execute revoked = %t; want both true", name, definer, publicRevoked)
		}
		if wantConfig := "search_path=pg_catalog, " + schema + ", pg_temp"; !config.Valid || config.String != wantConfig {
			t.Fatalf("%s: proconfig = %q, want %q", name, config.String, wantConfig)
		}
	}
	if seen != want {
		t.Fatalf("schema %s holds %d capture functions, want %d", schema, seen, want)
	}
}

// winnersProofRole names the NOLOGIN role the log-blind probes run as.
const winnersProofRole = "winners_trigger_proof_role"

// logBlindConn returns a pinned connection running as a login-like role that
// may move and delete scope rows and nothing else: no privilege on the dirty
// log, its sequence or fact_records. Other features' triggers on
// ingestion_scopes are disabled so only the two winners capture triggers run,
// and the dirty log starts empty. The role keeps the default TEMP privilege on
// the database, as an ordinary login does.
func logBlindConn(h *keyedHarness) *sql.Conn {
	h.t.Helper()
	role := winnersProofRole
	h.exec(`DROP ROLE IF EXISTS ` + role)
	h.exec(`CREATE ROLE ` + role + ` NOLOGIN`)
	h.t.Cleanup(func() {
		_, _ = h.db.ExecContext(context.Background(), `DROP OWNED BY `+role)
		_, _ = h.db.ExecContext(context.Background(), `DROP ROLE IF EXISTS `+role)
	})
	h.exec(`GRANT USAGE ON SCHEMA public TO ` + role)
	h.exec(`GRANT SELECT, UPDATE, DELETE ON ingestion_scopes TO ` + role)
	// Other features hang their own triggers on ingestion_scopes with their own
	// privilege needs; disable them so the role exercises only the two under test.
	rows, err := h.db.QueryContext(h.ctx, `SELECT tgname FROM pg_trigger
WHERE tgrelid = 'ingestion_scopes'::regclass AND NOT tgisinternal AND tgname NOT LIKE 'ingestion_scopes_winners_dirty_capture%'`)
	if err != nil {
		h.t.Fatalf("list scope triggers: %v", err)
	}
	var others []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			h.t.Fatalf("scan trigger name: %v", err)
		}
		others = append(others, name)
	}
	if err := rows.Close(); err != nil {
		h.t.Fatalf("close trigger list: %v", err)
	}
	for _, name := range others {
		h.exec(`ALTER TABLE ingestion_scopes DISABLE TRIGGER "` + name + `"`)
	}
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)

	conn, err := h.db.Conn(h.ctx)
	if err != nil {
		h.t.Fatalf("pin a connection: %v", err)
	}
	h.t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `RESET ROLE`)
		_ = conn.Close()
	})
	if _, err := conn.ExecContext(h.ctx, `SET ROLE `+role); err != nil {
		h.t.Fatalf("set role: %v", err)
	}
	return conn
}

// TestSupplyChainImpactWinnersResweepCaptureIgnoresCallerTempTablesLive is the
// pg_temp regression for the definer capture functions (#9805 review R1). A
// caller that may create temporary tables can shadow an unqualified relation
// name, because Postgres searches the session temp schema first unless pg_temp
// is listed in the search_path. Two probes run as a role with no privilege on
// the log or on fact_records:
//
//   - a temporary table named like the dirty log, then a pointer move: the
//     capture row must land in the real log, not the caller's table;
//   - a temporary table named like fact_records holding a forged finding of the
//     scope, then a scope delete: the real log must hold the scope's real
//     finding identifiers and none of the forged ones.
func TestSupplyChainImpactWinnersResweepCaptureIgnoresCallerTempTablesLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 definer pg_temp shadow proof")
	scopeA, scopeB := keyedSeedSettled(h)
	conn := logBlindConn(h)

	if _, err := conn.ExecContext(h.ctx, `CREATE TEMP TABLE supply_chain_impact_winners_dirty_log
(kind text, source text, scope_id text, generation_id text)`); err != nil {
		t.Fatalf("shadow the dirty log: %v", err)
	}
	if _, err := conn.ExecContext(h.ctx, `UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1`, scopeA); err != nil {
		t.Fatalf("pointer move beside a shadow log: %v", err)
	}
	var shadowed int
	if err := conn.QueryRowContext(h.ctx, `SELECT count(*) FROM pg_temp.supply_chain_impact_winners_dirty_log`).Scan(&shadowed); err != nil {
		t.Fatalf("count shadow log rows: %v", err)
	}
	if shadowed != 0 {
		t.Errorf("the pointer capture wrote %d rows into the caller's temporary table: the definer function resolves names through pg_temp", shadowed)
	}
	if real := h.count(`SELECT count(*) FROM public.supply_chain_impact_winners_dirty_log WHERE kind = 'scope' AND scope_id = $1`, scopeA); real != 1 {
		t.Errorf("the real log holds %d scope rows for the pointer move, want 1: the capture was diverted", real)
	}

	if _, err := conn.ExecContext(h.ctx, `CREATE TEMP TABLE fact_records
(scope_id text, fact_kind text, is_tombstone boolean, payload jsonb)`); err != nil {
		t.Fatalf("shadow fact_records: %v", err)
	}
	if _, err := conn.ExecContext(h.ctx, `INSERT INTO pg_temp.fact_records VALUES ($1, $2, false,
'{"package_id":"pkg:INJECTED","repository_id":"repo-injected","subject_digest":"","cve_id":"CVE-INJECTED","advisory_id":""}'::jsonb)`,
		scopeB, facts.ReducerSupplyChainImpactFindingFactKind); err != nil {
		t.Fatalf("forge a finding: %v", err)
	}
	if _, err := conn.ExecContext(h.ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeB); err != nil {
		t.Fatalf("scope delete beside a shadow fact_records: %v", err)
	}
	if forged := h.count(`SELECT count(*) FROM public.supply_chain_impact_winners_dirty_log WHERE package_id = 'pkg:INJECTED'`); forged != 0 {
		t.Fatalf("the real log holds %d forged identifiers: the definer function read the caller's temporary fact_records", forged)
	}
	if real := h.count(`SELECT count(*) FROM public.supply_chain_impact_winners_dirty_log WHERE kind = 'fact' AND source = 'activation'`); real < 1 {
		t.Fatalf("the real log holds %d fact rows for the scope delete, want at least 1", real)
	}
}
