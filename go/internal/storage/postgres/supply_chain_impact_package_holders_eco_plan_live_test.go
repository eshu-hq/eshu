// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const ecoPlanOwn = "eco:own"

// ecoPlanHolders are the holder scopes carrying the queried ecosystem.
var ecoPlanHolders = []string{"eco:hold-a", "eco:hold-b", "eco:hold-c"}

// ecoPlanQuery renders the shipped package-keyed holder query with the
// test-owned ecosystem list and asking scope inlined, so EXPLAIN takes no
// bind parameters (the keyRange precedent). It derives from the shipped
// constant: a hand-frozen copy would drift and go false-green.
func ecoPlanQuery(t *testing.T) string {
	t.Helper()
	q := strings.ReplaceAll(postgres.FindPackageHolderCandidatesQuery,
		"$1::text[]", "ARRAY['npm','node','nodejs','javascript','typescript']")
	return strings.ReplaceAll(q, "$2", "'"+ecoPlanOwn+"'")
}

// ecoPlanNoiseScopes is the number of bulk noise scopes. Each carries 200
// active rows of another ecosystem, so a per-scope probe reads 200 rows 300
// times while the eco index reads only the queried family's entries: the
// production-like many-scope shape where the planner prices the
// ecosystem-anchored path first.
const ecoPlanNoiseScopes = 300

// seedPackageHolderEcoPlan seeds the #11376 plan fixture: holder scopes with
// active affected-package rows in the queried npm family, superseded rows of
// the same family (retention keeps them and the breach shape reads them),
// and bulk noise scopes with many rows of another ecosystem so the queried
// family is a minority of the table — the shape that keeps the planner on an
// ecosystem-anchored access path instead of a per-scope probe. The bulk
// inserts are single statements: 90k row-by-row inserts would stall the
// contention gate.
func seedPackageHolderEcoPlan(t *testing.T, ctx context.Context, sqldb *sql.DB) {
	t.Helper()
	fanOutSeedScope(t, ctx, sqldb, ecoPlanOwn, "g1")
	for _, scope := range ecoPlanHolders {
		fanOutSeedScope(t, ctx, sqldb, scope, "g1", "g0")
	}
	for _, scope := range ecoPlanHolders {
		for i := 0; i < 150; i++ {
			manifestLiveSeedFact(t, ctx, sqldb, scope, fanOutGen(scope, "g1"),
				manifestLiveAffected(
					fmt.Sprintf("vulnerability.affected_package:eco-%s-g1-%d", scope, i),
					"npm", "pkg"))
		}
		for i := 0; i < 100; i++ {
			manifestLiveSeedFact(t, ctx, sqldb, scope, fanOutGen(scope, "g0"),
				manifestLiveAffected(
					fmt.Sprintf("vulnerability.affected_package:eco-%s-g0-%d", scope, i),
					"node", "pkg"))
		}
	}
	for i := 0; i < 10; i++ {
		manifestLiveSeedFact(t, ctx, sqldb, ecoPlanOwn, fanOutGen(ecoPlanOwn, "g1"),
			manifestLiveAffected(
				fmt.Sprintf("vulnerability.affected_package:eco-own-g1-%d", i),
				"npm", "pkg"))
	}
	if _, err := sqldb.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload)
SELECT 'eco:noise-' || g, 'vulnerability_intelligence', 'synthetic', 'eco:noise-' || g, 'synthetic',
  'eco:noise-' || g, now(), now(), 'active', '{}'::jsonb
FROM generate_series(1, $1) g`, ecoPlanNoiseScopes); err != nil {
		t.Fatalf("seed noise scopes: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'eco:noise-' || g || '/g1', 'eco:noise-' || g, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb
FROM generate_series(1, $1) g
UNION ALL
SELECT 'eco:noise-' || g || '/g0', 'eco:noise-' || g, 'synthetic', now(), now(), 'superseded', now(), '{}'::jsonb
FROM generate_series(1, $1) g`, ecoPlanNoiseScopes); err != nil {
		t.Fatalf("seed noise generations: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = scope_id || '/g1' WHERE scope_id LIKE 'eco:noise-%'`); err != nil {
		t.Fatalf("activate noise scopes: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'vulnerability.affected_package:eco-noise-' || g || '-' || n,
  'eco:noise-' || g, 'eco:noise-' || g || '/g1', 'vulnerability.affected_package',
  'vulnerability.affected_package:eco-noise-' || g || '-' || n, '1.0.0',
  'vulnerability_intelligence', 'eco', now(), now(), false,
  jsonb_build_object('ecosystem', 'go', 'package_name', 'pkg',
    'cve_id', 'CVE-2026-0001', 'purl', 'pkg:go/pkg', 'package_id', 'go://registry/pkg')
FROM generate_series(1, $1) g CROSS JOIN generate_series(1, 200) n`, ecoPlanNoiseScopes); err != nil {
		t.Fatalf("seed noise active facts: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'vulnerability.affected_package:eco-sup-' || g || '-' || n,
  'eco:noise-' || g, 'eco:noise-' || g || '/g0', 'vulnerability.affected_package',
  'vulnerability.affected_package:eco-sup-' || g || '-' || n, '1.0.0',
  'vulnerability_intelligence', 'eco', now(), now(), false,
  jsonb_build_object('ecosystem', 'golang', 'package_name', 'pkg',
    'cve_id', 'CVE-2026-0001', 'purl', 'pkg:golang/pkg', 'package_id', 'golang://registry/pkg')
FROM generate_series(1, $1) g CROSS JOIN generate_series(1, 100) n`, ecoPlanNoiseScopes); err != nil {
		t.Fatalf("seed noise superseded facts: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

// TestFindPackageHolderCandidatesEcoIndexPlanLive pins the plan the #11376
// bound rests on: the package-keyed holder read serves its ecosystem
// predicate from fact_records_vuln_pkg_eco_scope_gen_active_idx with the
// folded ecosystem expression and the ANY list in one Index Cond line, in
// both the default planner regime and the forced whole-index-bitmap regime
// that reproduces the many-scope breach choice. The seeded RED drops the
// index in a rolled-back transaction and asserts the pin fails on the old
// shape (whole affected-package partial index with the ecosystem as a heap
// filter), so the pin cannot pass while proving nothing.
func TestFindPackageHolderCandidatesEcoIndexPlanLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	seedPackageHolderEcoPlan(t, ctx, sqldb)
	conn, err := sqldb.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	explain := func(setting string) string {
		t.Helper()
		if setting != "" {
			if _, err := conn.ExecContext(ctx, `SELECT set_config('`+setting+`', 'off', false)`); err != nil {
				t.Fatalf("disable %s: %v", setting, err)
			}
			defer func() {
				_, _ = conn.ExecContext(ctx, `SELECT set_config('`+setting+`', 'on', false)`)
			}()
		}
		return keyRangeExplain(t, ctx, conn, `EXPLAIN (ANALYZE, BUFFERS) `+ecoPlanQuery(t))
	}
	plan := explain("")
	if why := packageHolderEcoPlanViolation(plan); why != "" {
		t.Fatalf("pinned plan: %s:\n%s", why, plan)
	}
	// The breach regime still adopts the anchor: with nested-loop and
	// sequential scan off, the planner prices the whole-index bitmap, and
	// the eco index must win that comparison.
	for _, setting := range []string{"enable_nestloop", "enable_seqscan"} {
		breach := explain(setting)
		if why := packageHolderEcoPlanViolation(breach); why != "" {
			t.Fatalf("pinned plan with %s off: %s:\n%s", setting, why, breach)
		}
	}
	// Seeded RED: without the index the same query falls back to the whole
	// affected-package partial index with the ecosystem as a heap filter.
	tx, err := sqldb.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin RED: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS `+packageHolderEcoIndex); err != nil {
		t.Fatalf("drop RED: %v", err)
	}
	rows, err := tx.QueryContext(ctx, `EXPLAIN `+ecoPlanQuery(t))
	if err != nil {
		t.Fatalf("explain RED: %v", err)
	}
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan RED: %v", err)
		}
		lines = append(lines, line)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("rows RED: %v", err)
	}
	red := strings.Join(lines, "\n")
	if why := packageHolderEcoPlanViolation(red); why == "" {
		t.Fatalf("seeded RED: the pin passed without %s; re-check what the store pins:\n%s", packageHolderEcoIndex, red)
	} else if !strings.Contains(why, packageHolderEcoIndex) {
		t.Fatalf("seeded RED: violation %q does not name the dropped index; the RED half proves the wrong shape:\n%s", why, red)
	}
}

// keyRangeExplain runs an EXPLAIN (ANALYZE, BUFFERS) statement on the given
// connection and returns the plan text. It mirrors the sibling key-range
// plan tests' explain shape so the pin reads the same output form.
func keyRangeExplain(t *testing.T, ctx context.Context, conn *sql.Conn, stmt string) string {
	t.Helper()
	rows, err := conn.QueryContext(ctx, stmt)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows explain: %v", err)
	}
	return strings.Join(lines, "\n")
}
