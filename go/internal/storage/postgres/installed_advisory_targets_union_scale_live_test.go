// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestOSPackageNarrowedQueryUsesTheIndexAtScaleLive proves the UNION narrowed
// read (#9859) against real Postgres at 50,000 rows plus the realistic filler
// mix: every cell of the key set is served by both expression indexes, reads
// few buffers, and removes few rows, instead of scanning every installed
// package the way the OR shape did (generic plan: primary-key scan, ~350,000
// rows removed per page, ~18,000 buffers).
//
// The #9859 source rows prove the source arm fires independently: a key that
// meets no binary prefix still returns the row whose source_package derives
// to it. The dense deep-cursor cell proves the arms seek from a cursor deep
// in the table instead of rescanning: 1,000 rows share one source key and the
// page starts halfway through them.
func TestOSPackageNarrowedQueryUsesTheIndexAtScaleLive(t *testing.T) {
	ctx, db := openCappedScopeLiveDB(t)
	seedCappedScanScope(t, ctx, db, 50_000)
	// Rows 7 and 5000 carry a source name, as dpkg rows built from another
	// source package do; every other seeded row keeps no source_package key.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_records SET payload = payload || jsonb_build_object('source_package',
  CASE fact_id WHEN 'os-package:7154:000007' THEN 'src-0007' ELSE 'src-5000' END)
WHERE fact_id IN ('os-package:7154:000007', 'os-package:7154:005000')`); err != nil {
		t.Fatalf("seed source-package rows: %v", err)
	}
	// A dense source key: 1,000 consecutive rows share one source name, so a
	// deep cursor still pages inside one arm's posting list. Their binary
	// names all truncate to pkg-4000, which no cell queries.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_records SET payload = payload || '{"source_package":"dense-src"}'
WHERE fact_id BETWEEN 'os-package:7154:040000' AND 'os-package:7154:040999'`); err != nil {
		t.Fatalf("seed dense source rows: %v", err)
	}
	// Production fact_records is dominated by other fact kinds. Without them the
	// generic plan sees a table of nothing but os_package rows and a primary-key
	// scan with a filter looks as cheap as the index, so seed the realistic mix.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'filler:7154:' || lpad(n::text, 7, '0'), $1, $2, 'repository', 'filler-' || n, 'synthetic', 'filler-' || n,
  $3::timestamptz, $3::timestamptz, FALSE, jsonb_build_object('n', n), 0
FROM generate_series(1, 300000) AS n`, cappedScanScope, cappedScanGeneration, replaceSetLiveNow); err != nil {
		t.Fatalf("seed filler rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	// pgx reaches a generic plan through its prepared-statement cache, so EXPLAIN a
	// prepared statement under force_generic_plan: bound parameters would show a
	// custom plan the reducer never runs.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SET plan_cache_mode = force_generic_plan`); err != nil {
		t.Fatalf("force generic plan: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `PREPARE os_narrowed AS `+postgres.ListOSPackageAdvisoryTargetsForPackagesQueryForTest()); err != nil {
		t.Fatalf("prepare narrowed query: %v", err)
	}
	// The real advisory key set (#9848): affectedOSPackageLookupKeys sends
	// os://<vendor>/<name> and the advisory purl's pkg:<type>/<vendor>/<name>
	// for every OS advisory. The os:// form never meets split_part(purl, '@', 1)
	// but rides in the same array. pkg-0007 and pkg-5000 each name exactly one
	// seeded row: lpad truncates n >= 10000 to its first four digits, so a name
	// below pkg-5000 such as pkg-1100 also names rows 11000-11009.
	const realKeys = `'{os://debian/pkg-0007,os://debian/pkg-5000,pkg:deb/debian/pkg-0007,pkg:deb/debian/pkg-5000}'`
	// The source-only key set (#9859): neither key meets a binary prefix, so
	// both rows return through the source arm alone.
	const sourceKeys = `'{os://debian/pkg-0007,pkg:deb/debian/src-0007}'`
	for _, tc := range []struct {
		keys   string
		cursor string
		want   int
	}{
		{realKeys, "", 2},
		{sourceKeys, "", 1},
		// The dense deep-cursor cell: 1,000 rows share the dense source key
		// and the cursor starts halfway through them, so only the source
		// arm's seek serves the page.
		{`'{pkg:deb/debian/dense-src}'`, "os-package:7154:040499", 500},
		// The key the advisory side sent before #9848: the versionless OSV purl
		// with its qualifiers, which no installed row's prefix equals.
		{`'{os://debian/pkg-0007,pkg:deb/debian/pkg-0007?arch=source&distro=bookworm}'`, "", 0},
	} {
		if got := countNarrowedRows(ctx, t, conn, tc.keys, tc.cursor); got != tc.want {
			t.Fatalf("narrowed query with keys %s cursor %q returned %d rows, want %d", tc.keys, tc.cursor, got, tc.want)
		}
	}
	for _, tc := range []struct {
		name   string
		keys   string
		cursor string
	}{
		{"realKeys", realKeys, ""},
		{"sourceKeys", sourceKeys, ""},
		{"denseDeepCursor", `'{pkg:deb/debian/dense-src}'`, "os-package:7154:040499"},
	} {
		plan := explainNarrowedPlan(ctx, t, conn, tc.keys, tc.cursor)
		t.Logf("narrowed query %s cell plan:\n%s", tc.name, plan)
		// Log the cell's measured costs first, so one run records every
		// cell for the evidence note even when a bound fails.
		t.Logf("narrowed query %s cell: buffers=%d removed=%d execution=%s ms",
			tc.name, planSharedBuffers(plan), planRowsRemoved(plan), planExecutionMS(plan))
		for _, want := range []string{
			"fact_records_os_package_purl_prefix_idx",
			"fact_records_os_package_source_purl_prefix_idx",
		} {
			if !strings.Contains(plan, want) {
				t.Errorf("%s cell plan lacks %q (one arm lost its index):\n%s", tc.name, want, plan)
			}
		}
		for _, banned := range []string{
			"Seq Scan on fact_records",
			"fact_records_pkey",
		} {
			if strings.Contains(plan, banned) {
				t.Errorf("%s cell plan contains %q (the read scans instead of seeking):\n%s", tc.name, banned, plan)
			}
		}
		if got := planSharedBuffers(plan); got > 1000 {
			t.Errorf("%s cell plan touches %d shared buffers, want at most 1000:\n%s", tc.name, got, plan)
		}
		if got := planRowsRemoved(plan); got > 1000 {
			t.Errorf("%s cell plan removes %d rows by filter, want at most 1000:\n%s", tc.name, got, plan)
		}
	}
}

// explainNarrowedPlan runs the prepared UNION narrowed read for one key set
// and cursor under EXPLAIN (ANALYZE, BUFFERS) and returns the plan text.
func explainNarrowedPlan(ctx context.Context, t *testing.T, conn *sql.Conn, keys, cursor string) string {
	t.Helper()
	rows, err := conn.QueryContext(ctx,
		`EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) EXECUTE os_narrowed(`+keys+`, '`+cursor+`', 500, '{debian}')`)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return strings.Join(plan, "\n")
}

var (
	planSharedHitPattern   = regexp.MustCompile(`shared hit=(\d+)`)
	planRowsRemovedPattern = regexp.MustCompile(`Rows Removed by (?:Join )?Filter: (\d+)`)
	planExecTimePattern    = regexp.MustCompile(`Execution Time: ([\d.]+) ms`)
)

// planSharedBuffers returns the outermost plan node's shared-hit buffers: a
// node's Buffers line already includes its children, so summing every node
// would count each page once per level. A seek serves a page from a few index
// and heap pages, while a scan touches one buffer per heap page of the table.
func planSharedBuffers(plan string) int {
	match := planSharedHitPattern.FindStringSubmatch(plan)
	if match == nil {
		return -1
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return -1
	}
	return n
}

// planRowsRemoved totals the rows every plan node filters out after reading
// them: an index-served arm removes none, while a primary-key scan with a
// filter removes every non-matching row of the table.
func planRowsRemoved(plan string) int {
	total := 0
	for _, match := range planRowsRemovedPattern.FindAllStringSubmatch(plan, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		total += n
	}
	return total
}

// planExecutionMS returns the plan's Execution Time line for the evidence
// note's before/after table.
func planExecutionMS(plan string) string {
	if match := planExecTimePattern.FindStringSubmatch(plan); match != nil {
		return match[1]
	}
	return "unknown"
}

// countNarrowedRows executes the prepared os_narrowed statement for one key
// array and cursor and counts the rows it returns.
func countNarrowedRows(ctx context.Context, t *testing.T, conn *sql.Conn, keys, cursor string) int {
	t.Helper()
	rows, err := conn.QueryContext(ctx, `EXECUTE os_narrowed(`+keys+`, '`+cursor+`', 500, '{debian}')`)
	if err != nil {
		t.Fatalf("execute narrowed query %s: %v", keys, err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read narrowed rows %s: %v", keys, err)
	}
	return count
}
