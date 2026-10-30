// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestOwnedPackageReadDrivesFromActiveGenerations is the hermetic guard for
// #9426: the active_dependencies CTE restricts the fact read to the active
// generation set exactly once, in every identity and alias mode, and the
// all-history statement the live differential compares against is the shipped
// statement minus that one predicate.
func TestOwnedPackageReadDrivesFromActiveGenerations(t *testing.T) {
	t.Parallel()

	for _, versionSpecific := range []bool{true, false} {
		for _, expandAliases := range []bool{true, false} {
			query := listOwnedPackageDependencyTargetsQuery(versionSpecific, expandAliases)
			if got := strings.Count(query, ownedPackageActiveGenerationPredicate); got != 1 {
				t.Fatalf("version=%v aliases=%v: active-generation predicate appears %d times, want 1:\n%s",
					versionSpecific, expandAliases, got, query)
			}
			chain, _, found := strings.Cut(query, "distinct_targets AS MATERIALIZED (")
			if !found || !strings.Contains(chain, ownedPackageActiveGenerationPredicate) {
				t.Fatalf("version=%v aliases=%v: predicate must sit in active_dependencies, before distinct_targets",
					versionSpecific, expandAliases)
			}
			history := ownedPackageAllHistoryQuery(versionSpecific, expandAliases)
			if history == query || strings.Contains(history, "ANY (ARRAY(SELECT active_generation_id") {
				t.Fatalf("version=%v aliases=%v: all-history query still restricts the fact read", versionSpecific, expandAliases)
			}
		}
	}
}

// ownedPackageAllHistoryQuery derives the pre-#9426 statement from the
// shipped one by removing the active-generation predicate, so the live
// differential always compares production text with production text minus one
// line, never a hand-kept copy.
func ownedPackageAllHistoryQuery(versionSpecific bool, expandAliases bool) string {
	return strings.Replace(listOwnedPackageDependencyTargetsQuery(versionSpecific, expandAliases),
		ownedPackageActiveGenerationPredicate, "", 1)
}

const (
	historyLiveGenerations = 12
	historyLivePackages    = 40
)

// historyLiveFixture adds a scope with eleven superseded generations and one
// active generation of the same 40 npm packages (the version moves with the
// generation), a package seen only in the oldest generation, a scope with no
// active generation, and a scope whose active_generation_id points at a
// generation that is not in status active. Only the 40 active rows are live
// inventory; everything else is history the read must not pay for.
func historyLiveFixture() string {
	return fmt.Sprintf(`
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES
    ('scope-hist', 'repository', 'git', 'repo-hist', 'git', 'repository:hist', now(), now(), 'active', 'gen-hist-%[1]d'),
    ('scope-pending', 'repository', 'git', 'repo-pending', 'git', 'repository:pending', now(), now(), 'active', NULL),
    ('scope-mismatch', 'repository', 'git', 'repo-mismatch', 'git', 'repository:mismatch', now(), now(), 'active', 'gen-mismatch');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'gen-hist-' || g, 'scope-hist', 'snapshot', now(), now(),
       CASE WHEN g = %[1]d THEN 'active' ELSE 'superseded' END
FROM generate_series(1, %[1]d) AS g
UNION ALL SELECT 'gen-pending', 'scope-pending', 'snapshot', now(), now(), 'pending'
UNION ALL SELECT 'gen-mismatch', 'scope-mismatch', 'snapshot', now(), now(), 'superseded';
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT d.fact_id, d.scope_id, d.generation_id, 'content_entity', 'stable-' || d.fact_id, 'git',
    'git', d.fact_id, '2026-10-28T12:00:00Z', now(), FALSE,
    jsonb_build_object(
        'entity_type', 'Variable',
        'entity_name', d.package_name,
        'repo_id', d.scope_id,
        'entity_metadata', jsonb_build_object(
            'config_kind', 'dependency',
            'package_manager', 'npm',
            'value', d.version,
            'lockfile', 'true'))
FROM (
    SELECT 'hist-' || g || '-' || n AS fact_id, 'scope-hist' AS scope_id, 'gen-hist-' || g AS generation_id,
           'hist-pkg-' || lpad(n::text, 2, '0') AS package_name, '1.' || g || '.0' AS version
    FROM generate_series(1, %[1]d) AS g, generate_series(0, %[2]d) AS n
    UNION ALL SELECT 'hist-only-old', 'scope-hist', 'gen-hist-1', 'hist-only-old', '0.1.0'
    UNION ALL SELECT 'pending-' || n, 'scope-pending', 'gen-pending', 'pending-pkg-' || n, '2.0.0'
    FROM generate_series(0, 9) AS n
    UNION ALL SELECT 'mismatch-' || n, 'scope-mismatch', 'gen-mismatch', 'mismatch-pkg-' || n, '3.0.0'
    FROM generate_series(0, 9) AS n
) AS d;
`, historyLiveGenerations, historyLivePackages-1)
}

// assertOwnedPackageReadFollowsActiveGenerations seeds generation history on
// top of the slot fixture and proves the #9426 contract on a real Postgres:
// the shipped statement returns exactly the rows of the all-history statement
// for every slot, and its fact_records scan reads no more rows than the active
// inventory while the all-history statement reads the history too.
func assertOwnedPackageReadFollowsActiveGenerations(ctx context.Context, t *testing.T, db *sql.DB, activeBefore int64) {
	t.Helper()
	if _, err := db.ExecContext(ctx, historyLiveFixture()); err != nil {
		t.Fatalf("seed owned-package history fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records; ANALYZE ingestion_scopes; ANALYZE scope_generations"); err != nil {
		t.Fatalf("analyze history fixture: %v", err)
	}
	active := activeBefore + historyLivePackages
	total := active
	shipped := listOwnedPackageDependencyTargetsQuery(true, true)
	history := ownedPackageAllHistoryQuery(true, true)
	filter := ownedPackageManagerFilterValues([]string{"npm", "rubygems"}, true, false)
	slots := workflow.OwnedPackageTargetSlotCount(total, slotLiveTargetLimit)

	var all []string
	for slot := int64(0); slot < slots; slot++ {
		args := []any{filter, int64(100000), slot, slotLiveTargetLimit}
		got := ownedPackageStatementRows(ctx, t, db, shipped, args)
		want := ownedPackageStatementRows(ctx, t, db, history, args)
		if !slices.Equal(got, want) {
			t.Fatalf("slot %d: active-generation read returned %d rows, all-history read %d; outputs must be identical",
				slot, len(got), len(want))
		}
		all = append(all, got...)
	}
	joined := strings.Join(all, "\n")
	if !strings.Contains(joined, "npm|hist-pkg-00|1.12.0|") {
		t.Fatalf("active history package hist-pkg-00@1.12.0 missing from the read")
	}
	for _, stale := range []string{"hist-only-old", "|1.11.0|", "pending-pkg-", "mismatch-pkg-"} {
		if strings.Contains(joined, stale) {
			t.Fatalf("read returned %q, which is not in an active generation", stale)
		}
	}

	args := []any{filter, int64(slotLiveTargetLimit + 1), int64(0), slotLiveTargetLimit}
	if rows := ownedPackageFactScanRows(ctx, t, db, history, args); rows <= active {
		t.Fatalf("all-history plan read %d fact rows, want more than the %d active rows; the fixture does not discriminate", rows, active)
	}
	// The predicate admits every generation a scope points at; scope-mismatch
	// points at a non-active generation, so its 10 rows are read and then
	// dropped by the status join. Superseded and pending rows are never read.
	pointed := active + 10
	if rows := ownedPackageFactScanRows(ctx, t, db, shipped, args); rows > pointed {
		t.Fatalf("shipped plan read %d fact rows, want at most the %d rows of pointed-at generations: the read follows generation history",
			rows, pointed)
	}
}

// ownedPackageStatementRows runs one owned-package statement and returns every
// output column of every row as one string per row, in statement order. It
// scans by column count, not by a hand-copied column list, so projecting
// another already-computed column (key_hash_low24, #9432) cannot break it.
func ownedPackageStatementRows(ctx context.Context, t *testing.T, db *sql.DB, query string, args []any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("run owned-package statement: %v", err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("owned-package columns: %v", err)
	}
	var out []string
	for rows.Next() {
		cells := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatalf("scan owned-package row: %v", err)
		}
		text := make([]string, len(cells))
		for i, cell := range cells {
			text[i] = ownedPackageCellText(cell)
		}
		out = append(out, strings.Join(text, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("owned-package rows: %v", err)
	}
	return out
}

// ownedPackageCellText renders one scanned cell the way the previous
// hand-copied scan did: text as-is, booleans and integers plainly, NULL as
// empty, so package-identity assertions read unchanged.
func ownedPackageCellText(cell any) string {
	switch value := cell.(type) {
	case nil:
		return ""
	case string:
		return value
	case []byte:
		return string(value)
	case bool:
		return strconv.FormatBool(value)
	case int64:
		return strconv.FormatInt(value, 10)
	default:
		return fmt.Sprintf("%v", value)
	}
}

// ownedPackageFactScanRows runs EXPLAIN (ANALYZE, FORMAT JSON) on a statement
// and sums actual rows times loops over every plan node that reads
// fact_records, which is the number of fact rows the statement paid for.
func ownedPackageFactScanRows(ctx context.Context, t *testing.T, db *sql.DB, query string, args []any) int64 {
	t.Helper()
	var raw string
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain owned-package statement: %v", err)
	}
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode explain JSON (%d plans): %v", len(plans), err)
	}
	var sum float64
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		if node["Relation Name"] == "fact_records" {
			rows, _ := node["Actual Rows"].(float64)
			loops, _ := node["Actual Loops"].(float64)
			sum += rows * loops
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	walk(plans[0].Plan)
	return int64(sum + 0.5)
}
