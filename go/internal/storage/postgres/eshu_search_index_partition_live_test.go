// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestEshuSearchIndexTermsPartitionCutoverMigrationLive(t *testing.T) {
	db, ctx := openSearchIndexPartitionProofDB(t)
	conn, schemaName := searchIndexPartitionProofConn(t, ctx, db)
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY
);
CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY
);
CREATE TABLE eshu_search_index_terms (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    term_key TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL,
    PRIMARY KEY (scope_id, generation_id, term_key, document_id)
);
INSERT INTO ingestion_scopes(scope_id) VALUES ('scope-a'), ('scope-b');
INSERT INTO scope_generations(generation_id) VALUES ('gen-a'), ('gen-b');
INSERT INTO eshu_search_index_terms(scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES
    ('scope-a', 'gen-a', 'doc-1', 'alpha', 'alpha', 2),
    ('scope-a', 'gen-a', 'doc-2', 'beta', 'beta', 1),
    ('scope-b', 'gen-b', 'doc-3', 'alpha', 'alpha', 4);
`); err != nil {
		t.Fatalf("seed unpartitioned proof schema %s: %v", schemaName, err)
	}

	for run := 1; run <= 2; run++ {
		if _, err := conn.ExecContext(ctx, MigrationSQL("partition_eshu_search_index_terms")); err != nil {
			t.Fatalf("run partition migration %d: %v", run, err)
		}
		assertSearchIndexTermsPartitionedLive(t, ctx, conn)
		assertSearchIndexTermsCutoverRowsLive(t, ctx, conn)
	}
}

func TestEshuSearchIndexTermsFreshBootstrapPartitionedLive(t *testing.T) {
	db, ctx := openSearchIndexPartitionProofDB(t)
	conn, _ := searchIndexPartitionProofConn(t, ctx, db)
	defer func() { _ = conn.Close() }()

	for _, name := range []string{
		"ingestion_scopes",
		"scope_generations",
		"fact_records",
		"eshu_search_index",
	} {
		if _, err := conn.ExecContext(ctx, MigrationSQL(name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	assertSearchIndexTermsPartitionedLive(t, ctx, conn)
}

func TestCopySearchIndexTermsRoutesThroughPartitionedParentLive(t *testing.T) {
	db, ctx := openSearchIndexPartitionProofDB(t)
	table := fmt.Sprintf("eshu_search_index_terms_copy_parent_%d", time.Now().UnixNano())
	createPartitionedSearchIndexTermsProofTable(t, ctx, db, table, 64)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
	})

	rows := []struct {
		scopeID      string
		generationID string
		documentIDs  []string
		terms        []string
		termKeys     []string
		frequencies  []int
	}{
		{"scope-copy-a", "gen-copy", []string{"doc-a1", "doc-a2"}, []string{"alpha", "beta"}, []string{"alpha", "beta"}, []int{2, 1}},
		{"scope-copy-b", "gen-copy", []string{"doc-b1", "doc-b2"}, []string{"alpha", "gamma"}, []string{"alpha", "gamma"}, []int{1, 3}},
		{"scope-copy-c", "gen-copy", []string{"doc-c1", "doc-c2"}, []string{"delta", "epsilon"}, []string{"delta", "epsilon"}, []int{5, 8}},
		{"scope-copy-d", "gen-copy", []string{"doc-d1", "doc-d2"}, []string{"zeta", "eta"}, []string{"zeta", "eta"}, []int{13, 21}},
	}
	var wantRows int64
	for _, row := range rows {
		copied, err := SQLDB{DB: db}.copySearchIndexTermsToTable(
			ctx,
			table,
			row.scopeID,
			row.generationID,
			row.documentIDs,
			row.terms,
			row.termKeys,
			row.frequencies,
		)
		if err != nil {
			t.Fatalf("copy scope %s through partitioned parent: %v", row.scopeID, err)
		}
		wantRows += int64(len(row.terms))
		if copied != int64(len(row.terms)) {
			t.Fatalf("copied rows for %s = %d, want %d", row.scopeID, copied, len(row.terms))
		}
	}

	var gotRows int64
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&gotRows); err != nil {
		t.Fatalf("count copied rows: %v", err)
	}
	if gotRows != wantRows {
		t.Fatalf("copied rows = %d, want %d", gotRows, wantRows)
	}

	partitionNames := distinctTableOIDsLive(t, ctx, db, table)
	if len(partitionNames) < 2 {
		t.Fatalf("COPY rows landed in %d partitions (%v), want at least 2", len(partitionNames), partitionNames)
	}

	var scatteredScopes int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`
SELECT count(*)
FROM (
    SELECT scope_id
    FROM %s
    GROUP BY scope_id
    HAVING count(DISTINCT tableoid::regclass::text) <> 1
) scattered
`, table)).Scan(&scatteredScopes); err != nil {
		t.Fatalf("check same-scope partition scattering: %v", err)
	}
	if scatteredScopes != 0 {
		t.Fatalf("%d scopes were scattered across multiple hash partitions", scatteredScopes)
	}
}

func TestEshuSearchIndexTermsGenerationClearPrunesScopePartitionLive(t *testing.T) {
	db, ctx := openSearchIndexPartitionProofDB(t)
	table := fmt.Sprintf("eshu_search_index_terms_clear_partition_%d", time.Now().UnixNano())
	createPartitionedSearchIndexTermsProofTable(t, ctx, db, table, 64)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
	})
	seedSearchIndexTermsPartitionPruneRows(t, ctx, db, table)
	if _, err := db.ExecContext(ctx, fmt.Sprintf("ANALYZE %s", table)); err != nil {
		t.Fatalf("analyze %s: %v", table, err)
	}

	expectedChild := childPartitionForScopeLive(t, ctx, db, table, "scope-prune-017")
	plan := explainSearchIndexPlanLive(
		t,
		ctx,
		db,
		fmt.Sprintf("DELETE FROM %s WHERE scope_id = $1 AND generation_id = $2", table),
		"scope-prune-017",
		"gen-prune",
	)
	childScans := scannedChildPartitionsLive(plan, table)
	if len(childScans) == 0 {
		t.Fatalf("clear plan scanned no child partition:\n%s", planJSONSummary(t, plan))
	}
	if len(childScans) >= 64 {
		t.Fatalf("clear plan scanned all partitions; want scope pruning:\n%s", planJSONSummary(t, plan))
	}
	if _, ok := childScans[expectedChild]; !ok {
		t.Fatalf("clear plan scanned %v, want target child %s:\n%s", sortedMapKeys(childScans), expectedChild, planJSONSummary(t, plan))
	}
}

func openSearchIndexPartitionProofDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	if os.Getenv("ESHU_SEARCH_INDEX_PARTITION_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_INDEX_PARTITION_LIVE=1 and ESHU_POSTGRES_DSN to run partition proof")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run partition proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return db, ctx
}

func searchIndexPartitionProofConn(t *testing.T, ctx context.Context, db *sql.DB) (*sql.Conn, string) {
	t.Helper()
	schemaName := fmt.Sprintf("eshu_search_partition_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %s", quoteIdentLive(schemaName))); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", quoteIdentLive(schemaName)))
	})
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire proof connection: %v", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", quoteIdentLive(schemaName))); err != nil {
		_ = conn.Close()
		t.Fatalf("set proof search_path: %v", err)
	}
	return conn, schemaName
}

func assertSearchIndexTermsPartitionedLive(t *testing.T, ctx context.Context, conn *sql.Conn) {
	t.Helper()
	var relkind string
	if err := conn.QueryRowContext(ctx, `
SELECT c.relkind::text
FROM pg_class c
WHERE c.oid = 'eshu_search_index_terms'::regclass
`).Scan(&relkind); err != nil {
		t.Fatalf("read terms relkind: %v", err)
	}
	if relkind != "p" {
		t.Fatalf("eshu_search_index_terms relkind = %q, want partitioned table", relkind)
	}
	var partitions int
	if err := conn.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_inherits
WHERE inhparent = 'eshu_search_index_terms'::regclass
`).Scan(&partitions); err != nil {
		t.Fatalf("count terms partitions: %v", err)
	}
	if partitions != 64 {
		t.Fatalf("partition count = %d, want 64", partitions)
	}
	var pkeyCount int
	if err := conn.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_constraint
WHERE conrelid = 'eshu_search_index_terms'::regclass
  AND conname = 'eshu_search_index_terms_pkey'
`).Scan(&pkeyCount); err != nil {
		t.Fatalf("count terms pkey: %v", err)
	}
	if pkeyCount != 1 {
		t.Fatalf("pkey count = %d, want 1", pkeyCount)
	}
}

func assertSearchIndexTermsCutoverRowsLive(t *testing.T, ctx context.Context, conn *sql.Conn) {
	t.Helper()
	var diffCount int
	if err := conn.QueryRowContext(ctx, `
WITH expected(scope_id, generation_id, document_id, term_key, term, term_frequency) AS (
    VALUES
        ('scope-a', 'gen-a', 'doc-1', 'alpha', 'alpha', 2),
        ('scope-a', 'gen-a', 'doc-2', 'beta', 'beta', 1),
        ('scope-b', 'gen-b', 'doc-3', 'alpha', 'alpha', 4)
)
SELECT count(*)
FROM (
    (SELECT * FROM expected EXCEPT ALL SELECT scope_id, generation_id, document_id, term_key, term, term_frequency FROM eshu_search_index_terms)
    UNION ALL
    (SELECT scope_id, generation_id, document_id, term_key, term, term_frequency FROM eshu_search_index_terms EXCEPT ALL SELECT * FROM expected)
) diff
`).Scan(&diffCount); err != nil {
		t.Fatalf("compare cutover rows: %v", err)
	}
	if diffCount != 0 {
		t.Fatalf("cutover row diff count = %d, want 0", diffCount)
	}
}

func createPartitionedSearchIndexTermsProofTable(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	table string,
	partitions int,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
CREATE TABLE %s (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    term_key TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL,
    PRIMARY KEY (scope_id, generation_id, term_key, document_id)
) PARTITION BY HASH (scope_id)
`, table)); err != nil {
		t.Fatalf("create partitioned table %s: %v", table, err)
	}
	for remainder := 0; remainder < partitions; remainder++ {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(
			"CREATE TABLE %s_p%02d PARTITION OF %s FOR VALUES WITH (MODULUS %d, REMAINDER %d)",
			table,
			remainder,
			table,
			partitions,
			remainder,
		)); err != nil {
			t.Fatalf("create partition %d for %s: %v", remainder, table, err)
		}
	}
}

func seedSearchIndexTermsPartitionPruneRows(t *testing.T, ctx context.Context, db *sql.DB, table string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s(scope_id, generation_id, document_id, term_key, term, term_frequency)
SELECT
    'scope-prune-' || lpad(s::text, 3, '0'),
    'gen-prune',
    'doc-' || lpad(d::text, 4, '0'),
    'term-' || lpad(tk::text, 4, '0'),
    'term-' || lpad(tk::text, 4, '0'),
    1
FROM generate_series(0, 63) AS s,
     generate_series(0, 19) AS d,
     generate_series(0, 19) AS tk
`, table)); err != nil {
		t.Fatalf("seed prune rows: %v", err)
	}
}

func distinctTableOIDsLive(t *testing.T, ctx context.Context, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT DISTINCT tableoid::regclass::text
FROM %s
ORDER BY 1
`, table))
	if err != nil {
		t.Fatalf("list partitions for %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan partition name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate partition names: %v", err)
	}
	return names
}

func childPartitionForScopeLive(t *testing.T, ctx context.Context, db *sql.DB, table string, scopeID string) string {
	t.Helper()
	var child string
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`
SELECT tableoid::regclass::text
FROM %s
WHERE scope_id = $1
LIMIT 1
`, table), scopeID).Scan(&child); err != nil {
		t.Fatalf("read child partition for scope %s: %v", scopeID, err)
	}
	return child
}

func explainSearchIndexPlanLive(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) string {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var raw []byte
	if err := tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain query: %v", err)
	}
	return string(raw)
}

func scannedChildPartitionsLive(raw string, parent string) map[string]struct{} {
	var roots []struct {
		Plan searchIndexPartitionPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &roots); err != nil || len(roots) == 0 {
		return map[string]struct{}{}
	}
	scanned := map[string]struct{}{}
	collectScannedChildPartitionsLive(roots[0].Plan, parent, scanned)
	return scanned
}

type searchIndexPartitionPlanNode struct {
	RelationName string                         `json:"Relation Name"`
	Plans        []searchIndexPartitionPlanNode `json:"Plans"`
}

func collectScannedChildPartitionsLive(node searchIndexPartitionPlanNode, parent string, scanned map[string]struct{}) {
	if strings.HasPrefix(node.RelationName, parent+"_p") {
		scanned[node.RelationName] = struct{}{}
	}
	for _, child := range node.Plans {
		collectScannedChildPartitionsLive(child, parent, scanned)
	}
}

func quoteIdentLive(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sortedMapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
