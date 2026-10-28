// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8995 disposable PostgreSQL 18 package-only anchor plan and row-identity proof, enrolled in the live-postgres-readiness runner; local run passed in 12.94s of test time on PostgreSQL 18.6 on 2026-10-28
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	// packageAnchorScopes times packageAnchorFactsPerScope is the 200,000-row
	// corpus the issue's shim proved the slow shape on: 4 ecosystems by 500
	// names cycling together give 500 distinct keys of 400 rows each, and the
	// 5 target keys match 2,000 sidecar rows (1,800 live facts after
	// tombstones).
	packageAnchorScopes        = 200
	packageAnchorFactsPerScope = 1000
	// packageAnchorLookupIndex is the sidecar index whose leading columns are
	// exactly the target keys; the fixed anchor must probe it with the target
	// keys in its Index Cond instead of joining every active sidecar row
	// first and filtering after.
	packageAnchorLookupIndex = "package_manifest_consumption_keys_lookup_idx"
)

// previousPackageAnchorLeg is the package-only anchor leg as it stood before
// #8995 (production whitespace is SQL-insignificant at the splice boundary):
// it scans every active sidecar row, joins fact/scope/generation, and only
// then applies the target keys in an EXISTS filter. The differential test
// splices it back over the rewritten leg, so everything else is derived from
// the shipped ListReadinessQuery and cannot drift.
const previousPackageAnchorLeg = `SELECT fact.fact_id, fact.observed_at
        FROM package_manifest_consumption_keys AS consumption
        JOIN fact_records AS fact
          ON fact.fact_id = consumption.fact_id
         AND fact.scope_id = consumption.scope_id
         AND fact.generation_id = consumption.generation_id
        JOIN ingestion_scopes AS scope
          ON scope.scope_id = consumption.scope_id
         AND scope.active_generation_id = consumption.generation_id
        JOIN scope_generations AS generation
          ON generation.scope_id = consumption.scope_id
         AND generation.generation_id = consumption.generation_id
        WHERE $20
          AND fact.is_tombstone = FALSE
          AND generation.status = 'active'
          AND ($11 = '' OR consumption.repository_id = $11)
          AND EXISTS (
              SELECT 1
              FROM target_consumption_keys AS target
              WHERE target.ecosystem = consumption.ecosystem
                AND target.package_name = consumption.package_name
          )
        `

// previousPackageAnchorReadinessQuery rebuilds the pre-#8995 readiness
// statement by splicing the previous leg over the rewritten one.
func previousPackageAnchorReadinessQuery(t *testing.T) string {
	t.Helper()
	const start = "SELECT fact.fact_id, fact.observed_at"
	const end = ") AS manifest"
	if strings.Count(ListReadinessQuery, start) != 1 || strings.Count(ListReadinessQuery, end) != 1 {
		t.Fatal("package anchor splice markers not found exactly once in readiness query")
	}
	i := strings.Index(ListReadinessQuery, start)
	j := strings.Index(ListReadinessQuery, end)
	if j < i {
		t.Fatal("package anchor splice markers out of order in readiness query")
	}
	if !strings.Contains(ListReadinessQuery[i:j], "package_manifest_target_match AS MATERIALIZED") {
		t.Fatal("splice span does not cover the rewritten package anchor leg")
	}
	if !strings.Contains(previousPackageAnchorLeg, "EXISTS (") || strings.Contains(previousPackageAnchorLeg, "MATERIALIZED") {
		t.Fatal("previous package anchor leg is not the pre-#8995 EXISTS shape")
	}
	return ListReadinessQuery[:i] + previousPackageAnchorLeg + ListReadinessQuery[j:]
}

// packageAnchorTarget returns the target a production resolution would bind
// for these inputs (built with packageidentity.ConsumptionKeys, so the
// (ecosystem, package_name) pairs are exactly what resolution emits;
// "golang" normalizes to "gomod", which the corpus stores). Every key carries
// the queried package ID: uniqueReadinessTargetKeys drops keys with an empty
// package ID, so omitting it would silently bind zero target keys and the
// anchor would match nothing.
func packageAnchorTarget(t *testing.T) readinessTarget {
	t.Helper()
	const packageID = "npm://registry.npmjs.org/pkg-1"
	var keys []readinessTargetKey
	for _, input := range [][2]string{
		{"npm", "pkg-1"},
		{"pypi", "pkg-2"},
		{"cargo", "pkg-3"},
		{"golang", "pkg-4"},
		{"npm", "pkg-5"},
	} {
		derived := packageidentity.ConsumptionKeys(input[0], input[1])
		if len(derived) == 0 {
			t.Fatalf("ConsumptionKeys(%q, %q) is empty, want the production target key", input[0], input[1])
		}
		for _, key := range derived {
			keys = append(keys, readinessTargetKey{packageID: packageID, key: key})
		}
	}
	target := readinessTarget{PackageIDs: []string{packageID}, Keys: uniqueReadinessTargetKeys(keys)}
	if len(target.Keys) == 0 {
		t.Fatal("package anchor target has no keys, the anchor would match nothing")
	}
	return target
}

// seedPackageAnchorCorpus builds the issue's 200,000-row shape: 200 scopes of
// 1,000 content_entity dependency facts with one sidecar key row each, 4
// ecosystems by 500 names (2,000 distinct keys), every 40th fact a tombstone.
// Payloads carry the dependency Variable shape so the corpus stays
// backfill-consistent (the sidecar rows are what the backfill would derive).
func seedPackageAnchorCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	facts := packageAnchorScopes * packageAnchorFactsPerScope
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT 'pkg-anchor-scope-' || s, 'repository', 'git', 'pkg-anchor-repo-' || s, 'git', 'pkg-anchor-repo-' || s,
    clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM generate_series(1, %d) AS s;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'pkg-anchor-scope-' || s, 'pkg-anchor-gen-' || s, 'sync', clock_timestamp(), clock_timestamp(), 'active'
FROM generate_series(1, %d) AS s;
UPDATE ingestion_scopes SET active_generation_id = 'pkg-anchor-gen-' || substring(scope_id from 18)
WHERE scope_id LIKE 'pkg-anchor-scope-%%';
`, packageAnchorScopes, packageAnchorScopes)); err != nil {
		t.Fatalf("seed package anchor scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'pkg-anchor-fact-' || g,
    'pkg-anchor-scope-' || ((g-1)/%[1]d+1),
    'pkg-anchor-gen-' || ((g-1)/%[1]d+1),
    'content_entity', 'pkg-anchor-fact-' || g, 'git', 'pkg-anchor-fact-' || g,
    clock_timestamp(), clock_timestamp(), (g%%40 = 0),
    jsonb_build_object(
        'repo_id', 'pkg-anchor-repo-' || ((g-1)/%[1]d+1),
        'entity_type', 'Variable',
        'entity_name', 'pkg-' || (1+g%%500),
        'entity_metadata', jsonb_build_object(
            'config_kind', 'dependency',
            'package_manager', (ARRAY['npm','pypi','cargo','gomod'])[1+g%%4]))
FROM generate_series(1, %[2]d) AS g;
INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
SELECT 'pkg-anchor-fact-' || g,
    'pkg-anchor-scope-' || ((g-1)/%[1]d+1),
    'pkg-anchor-gen-' || ((g-1)/%[1]d+1),
    'pkg-anchor-repo-' || ((g-1)/%[1]d+1),
    (ARRAY['npm','pypi','cargo','gomod'])[1+g%%4],
    'pkg-' || (1+g%%500)
FROM generate_series(1, %[2]d) AS g;
ANALYZE fact_records;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;
ANALYZE package_manifest_consumption_keys;
`, packageAnchorFactsPerScope, facts)); err != nil {
		t.Fatalf("seed package anchor facts: %v", err)
	}
}

// packageAnchorScan is one sidecar read with the index condition that
// drives it. A Bitmap Heap Scan carries no index of its own: the condition
// lives on its Bitmap Index Scan child, so the walker merges the child in.
type packageAnchorScan struct {
	node      repoArmPlanNode
	indexName string
	indexCond string
}

// packageAnchorSidecarScans returns every plan node that reads the
// consumption sidecar.
func packageAnchorSidecarScans(node repoArmPlanNode) []packageAnchorScan {
	var out []packageAnchorScan
	if node.RelationName == "package_manifest_consumption_keys" {
		scan := packageAnchorScan{node: node, indexName: node.IndexName, indexCond: node.IndexCond}
		if node.NodeType == "Bitmap Heap Scan" {
			for _, bitmap := range packageAnchorBitmapIndexScans(node) {
				scan.indexName = bitmap.IndexName
				scan.indexCond = bitmap.IndexCond
				break
			}
		}
		out = append(out, scan)
	}
	for _, child := range node.Plans {
		out = append(out, packageAnchorSidecarScans(child)...)
	}
	return out
}

// packageAnchorBitmapIndexScans returns the bitmap index probes below a
// bitmap heap scan.
func packageAnchorBitmapIndexScans(node repoArmPlanNode) []repoArmPlanNode {
	var out []repoArmPlanNode
	if node.NodeType == "Bitmap Index Scan" && node.IndexName != "" {
		out = append(out, node)
	}
	for _, child := range node.Plans {
		out = append(out, packageAnchorBitmapIndexScans(child)...)
	}
	return out
}

// explainText runs a multi-row EXPLAIN and returns its full text: EXPLAIN
// returns one row per plan line, so a single-row scan would keep only the
// first line.
func explainText(t *testing.T, ctx context.Context, conn *sql.Conn, statement string) string {
	t.Helper()
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		t.Fatalf("EXPLAIN readiness: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var sb strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan lines: %v", err)
	}
	return sb.String()
}

// assertPackageAnchorPlan fails unless the generic plan of statement reads
// the consumption sidecar only through the lookup index with the target keys
// in its Index Cond, probed at most once per target key: the anchor's cost
// follows the number of matching keys, not the number of active sidecar rows
// or scopes. It runs under force_generic_plan (set by the caller on conn):
// the custom plan already folds the bind values into the probe, so only the
// generic plan (the pgx prepared-statement path after 5 executions) can catch
// the regression.
func assertPackageAnchorPlan(t *testing.T, ctx context.Context, conn *sql.Conn, label, statement string, args []any, targetKeys int) {
	t.Helper()
	name := "pkg_anchor_" + label
	if _, err := conn.ExecContext(ctx, "PREPARE "+name+"("+
		"text[], text[], text[], text[], text[], text[], text[], text[], "+
		"text, text, text, text, text, text, text[], text[], text[], text[], text[], boolean) AS "+statement); err != nil {
		t.Fatalf("PREPARE %s: %v", label, err)
	}
	execute := repoArmExecuteStatement(t, name, args)

	var rawJSON []byte
	if err := conn.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+execute).Scan(&rawJSON); err != nil {
		t.Fatalf("EXPLAIN %s (generic plan): %v", label, err)
	}
	// Generic-proof: the anchor leg gates on the $20 parameter, which a
	// custom plan would fold away with the bound literal. ($17/$18 ride
	// inside UNNEST and never render as $n in any plan.)
	if !strings.Contains(string(rawJSON), "$20") {
		t.Fatalf("%s: expected a generic plan (parameter $20 left unbound in the plan)", label)
	}
	root := decodeRepoArmPlan(t, rawJSON)
	scans := packageAnchorSidecarScans(root)
	if len(scans) == 0 {
		t.Fatalf("%s: generic plan reads no package_manifest_consumption_keys scan, want the target-driven probe", label)
	}
	for _, scan := range scans {
		t.Logf("%s sidecar scan: node=%s index=%q cond=%q filter=%q loops=%.0f buffers=%d",
			label, scan.node.NodeType, scan.indexName, scan.indexCond, scan.node.Filter,
			scan.node.ActualLoops, scan.node.SharedHit+scan.node.SharedRead)
		if scan.indexName != packageAnchorLookupIndex {
			t.Errorf("%s: sidecar read uses %s %q, want index %s",
				label, scan.node.NodeType, scan.indexName, packageAnchorLookupIndex)
		}
		if !strings.Contains(scan.indexCond, "ecosystem") || !strings.Contains(scan.indexCond, "package_name") {
			t.Errorf("%s: sidecar %q Index Cond %q does not apply the target keys (ecosystem, package_name)",
				label, scan.indexName, scan.indexCond)
		}
		if scan.node.ActualLoops > float64(targetKeys) {
			t.Errorf("%s: sidecar %q ran %.0f loops, want <= %d (one probe per target key, not per scope or active row)",
				label, scan.indexName, scan.node.ActualLoops, targetKeys)
		}
	}
	t.Logf("%s: sidecar scans=%d execution_ms=%.3f shared_buffers=%d",
		label, len(scans), executionTimeMs(t, rawJSON), root.SharedHit+root.SharedRead)
	t.Logf("%s plan text:\n%s", label, explainText(t, ctx, conn, "EXPLAIN (ANALYZE, BUFFERS) "+execute))
}

// executionTimeMs reads the top-level Execution Time of one EXPLAIN
// (ANALYZE, FORMAT JSON) document for the test log.
func executionTimeMs(t *testing.T, raw []byte) float64 {
	t.Helper()
	var plans []struct {
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan execution time: count=%d", len(plans))
	}
	return plans[0].ExecutionTime
}

// packageAnchorConsumptionCount is the obviously-correct pin for the anchor:
// distinct live facts behind sidecar rows matching the target keys.
func packageAnchorConsumptionCount(t *testing.T, ctx context.Context, db packageAnchorQueryer, args []any) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT consumption.fact_id)
FROM package_manifest_consumption_keys AS consumption
JOIN fact_records AS fact
  ON fact.fact_id = consumption.fact_id
 AND fact.scope_id = consumption.scope_id
 AND fact.generation_id = consumption.generation_id
JOIN ingestion_scopes AS scope
  ON scope.scope_id = consumption.scope_id
 AND scope.active_generation_id = consumption.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = consumption.scope_id
 AND generation.generation_id = consumption.generation_id
WHERE fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (consumption.ecosystem, consumption.package_name) IN (
      ('npm', 'pkg-1'), ('pypi', 'pkg-2'), ('cargo', 'pkg-3'),
      ('gomod', 'pkg-4'), ('npm', 'pkg-5'))`).Scan(&count); err != nil {
		t.Fatalf("pin package anchor count: %v", err)
	}
	return count
}

type packageAnchorQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// packageAnchorStatement derives the package-only anchor leg as an executable
// statement from the shipped ListReadinessQuery: everything before the final
// SELECT (all CTEs, including target_consumption_keys) plus a read-only tail
// that reads only the anchor's aggregate. Nothing is hand-copied, so the
// statement cannot drift from production; the hermetic guards below prove the
// derivation on every run.
func packageAnchorStatement(t *testing.T) string {
	t.Helper()
	const boundary = "\nSELECT family, fact_count, latest_observed_at"
	if strings.Count(ListReadinessQuery, boundary) != 1 {
		t.Fatalf("anchor statement boundary %q found %d times, want exactly once", boundary, strings.Count(ListReadinessQuery, boundary))
	}
	head, _, _ := strings.Cut(ListReadinessQuery, boundary)
	if !strings.HasPrefix(ListReadinessQuery, head) || head == ListReadinessQuery {
		t.Fatal("anchor statement head is not a strict prefix of the shipped query")
	}
	return head + "\nSELECT * FROM package_manifest_dependency"
}

// TestSupplyChainImpactReadinessPackageAnchorLive is the #8995 proof for a
// package-only anchor ($11 empty, $20 true) at the issue's 200,000-row scale.
// Before the fix the anchor joined every active sidecar row to fact_records
// and applied the target keys last (a join filter removing ~970k rows), so
// this test fails unless every sidecar read probes the lookup index with the
// target keys in its Index Cond. It runs under force_generic_plan: the custom
// plan already folds the bind values into the probe, so only the generic plan
// (the pgx prepared-statement path after 5 executions) can catch the
// regression. Row identity between the rewritten and previous statements over
// the same fixture proves the fence changes no evidence.
func TestSupplyChainImpactReadinessPackageAnchorLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 10*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedPackageAnchorCorpus(t, ctx, db)

	target := packageAnchorTarget(t)
	query := ReadinessQuery{PackageID: "npm://registry.npmjs.org/pkg-1"}
	args := readinessArgsForTarget(target, query)
	if resolved := args[len(args)-1]; resolved != true {
		t.Fatalf("$20 = %v, want true (package anchor must take the key arm)", resolved)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn(): %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force generic plan: %v", err)
	}
	// The anchor leg derived from the shipped query is the acceptance
	// subject; the full statement proves the same shape end to end.
	assertPackageAnchorPlan(t, ctx, conn, "anchor", packageAnchorStatement(t), args, len(target.Keys))
	assertPackageAnchorPlan(t, ctx, conn, "statement", ListReadinessQuery, args, len(target.Keys))

	previous := previousPackageAnchorReadinessQuery(t)
	got := readinessResultRows(t, ctx, db, ListReadinessQuery, args...)
	want := readinessResultRows(t, ctx, db, previous, args...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rewritten readiness rows differ from the previous statement\n got=%v\nwant=%v", got, want)
	}
	pinned := packageAnchorConsumptionCount(t, ctx, db, args)
	if pinned == 0 {
		t.Fatal("pinned package anchor count is 0, the fixture matches nothing")
	}
	if got := productionPackageConsumptionCount(t, ctx, db, args...); got != pinned {
		t.Errorf("production package.consumption fact count = %d, want pinned %d", got, pinned)
	}
	t.Logf("package.consumption fact count = %d (differential diff 0)", pinned)
}

// productionPackageConsumptionCount reads the package.consumption family count
// out of the shipped statement, so the differential cannot pass on two
// equally wrong statements. Two CTEs emit the family (correlation and
// manifest); like readSupplyChainImpactReadiness it sums them.
func productionPackageConsumptionCount(t *testing.T, ctx context.Context, db *sql.DB, args ...any) int {
	t.Helper()
	rows, err := db.QueryContext(ctx, ListReadinessQuery, args...)
	if err != nil {
		t.Fatalf("query production readiness: %v", err)
	}
	defer func() { _ = rows.Close() }()
	total := 0
	seen := false
	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incomplete sql.NullBool
		var reasons array.StringArray
		var a, b, c sql.NullString
		if err := rows.Scan(&family, &factCount, &latest, &incomplete, &reasons, &a, &b, &c); err != nil {
			t.Fatalf("scan production row: %v", err)
		}
		if family == string(EvidenceFamilyPackageConsumption) {
			total += factCount
			seen = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("production rows: %v", err)
	}
	if !seen {
		t.Fatal("production readiness returned no package.consumption family")
	}
	return total
}
