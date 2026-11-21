// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10881 hermetic HASH(scope_id) partition theory proof for fact_records: pruning, divided-vs-undivided row equality, PK dedup-semantics change, and unscoped fan-out shape on throwaway tables; reads ESHU_POSTGRES_TEST_DSN
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestFactRecordsHashPartitionProofLive is the #10881 P1 contract test.
// It proves the partitioning mechanism on throwaway tables shaped like
// fact_records (fact_id + scope_id identity, scope-bounded secondary
// index), so no production table, index, or migration is touched. The
// modulus here (8) proves the mechanism only; the production modulus
// choice (32 vs 64) is recorded with measurements in
// docs/internal/design/10881-fact-records-hash-partition-proof.md and is
// proven at representative scale by the P5 rehearsal, not by this test.
//
// It locks in four facts the P4 implementation must preserve:
//  1. A scope-bounded read prunes to exactly one partition.
//  2. The divided copy returns exactly the undivided rows for a scope.
//  3. PK (scope_id, fact_id) accepts a fact_id under a second scope while
//     PK (fact_id) rejects it: the dedup-semantics change, which is why
//     the real-data collision census must read zero before any migration.
//  4. A fact_id-alone join fans out across partitions: the reader-census
//     obligation, not a shape the migration may ignore.
func TestFactRecordsHashPartitionProofLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the fact partition proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer func() { _ = conn.Close() }()

	stamp := time.Now().UnixNano()
	whole := fmt.Sprintf("fact_partproof_whole_%d", stamp)
	divided := fmt.Sprintf("fact_partproof_div_%d", stamp)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(fmt.Sprintf(`CREATE TABLE %s (
		fact_id TEXT PRIMARY KEY,
		scope_id TEXT NOT NULL,
		generation_id TEXT NOT NULL,
		fact_kind TEXT NOT NULL
	)`, whole))
	exec(fmt.Sprintf(`CREATE INDEX %s_scope_gen_idx ON %s (scope_id, generation_id, fact_kind)`, whole, whole))
	exec(fmt.Sprintf(`CREATE TABLE %s (
		fact_id TEXT NOT NULL,
		scope_id TEXT NOT NULL,
		generation_id TEXT NOT NULL,
		fact_kind TEXT NOT NULL,
		PRIMARY KEY (scope_id, fact_id)
	) PARTITION BY HASH (scope_id)`, divided))
	for i := 0; i < 8; i++ {
		exec(fmt.Sprintf(`CREATE TABLE %s_p%d PARTITION OF %s FOR VALUES WITH (MODULUS 8, REMAINDER %d)`, divided, i, divided, i))
	}
	exec(fmt.Sprintf(`CREATE INDEX %s_scope_gen_idx ON %s (scope_id, generation_id, fact_kind)`, divided, divided))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, divided))
		_, _ = db.ExecContext(cleanupCtx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, whole))
	})

	// Seed: 8 scopes x 25 facts plus one skewed scope of 120 facts.
	for s := 0; s < 8; s++ {
		for f := 0; f < 25; f++ {
			id := fmt.Sprintf("proof-fact-%d-%d", s, f)
			scope := fmt.Sprintf("proof-scope-%d", s)
			exec(fmt.Sprintf(`INSERT INTO %s (fact_id, scope_id, generation_id, fact_kind) VALUES ($1, $2, 'gen-A', 'file')`, whole), id, scope)
			exec(fmt.Sprintf(`INSERT INTO %s (fact_id, scope_id, generation_id, fact_kind) VALUES ($1, $2, 'gen-A', 'file')`, divided), id, scope)
		}
	}
	for f := 0; f < 120; f++ {
		id := fmt.Sprintf("proof-giant-%d", f)
		exec(fmt.Sprintf(`INSERT INTO %s (fact_id, scope_id, generation_id, fact_kind) VALUES ($1, 'proof-scope-giant', 'gen-A', 'vuln')`, whole), id)
		exec(fmt.Sprintf(`INSERT INTO %s (fact_id, scope_id, generation_id, fact_kind) VALUES ($1, 'proof-scope-giant', 'gen-A', 'vuln')`, divided), id)
	}
	exec(fmt.Sprintf(`VACUUM ANALYZE %s`, whole))
	exec(fmt.Sprintf(`VACUUM ANALYZE %s`, divided))

	// 1+2. Scoped read prunes to one partition and matches the whole table.
	planRows := func(query string, args ...any) []string {
		t.Helper()
		rows, err := conn.QueryContext(ctx, query, args...)
		if err != nil {
			t.Fatalf("plan query: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan plan: %v", err)
			}
			out = append(out, line)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("plan rows: %v", err)
		}
		return out
	}
	plan := planRows(fmt.Sprintf(`EXPLAIN (COSTS OFF) SELECT fact_id FROM %s WHERE scope_id = $1 AND generation_id = 'gen-A'`, divided), "proof-scope-3")
	seen := map[string]bool{}
	for _, line := range plan {
		if strings.Contains(line, divided+"_p") {
			start := strings.Index(line, divided+"_p")
			seen[line[start:]] = true
		}
	}
	if len(seen) != 1 {
		t.Fatalf("scoped read touches %d partitions, want exactly 1: %v", len(seen), plan)
	}
	for _, table := range []string{whole, divided} {
		rows, err := conn.QueryContext(ctx, fmt.Sprintf(`SELECT fact_id FROM %s WHERE scope_id = $1 AND generation_id = 'gen-A' ORDER BY fact_id`, table), "proof-scope-3")
		if err != nil {
			t.Fatalf("scoped read on %s: %v", table, err)
		}
		var got []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan: %v", err)
			}
			got = append(got, id)
		}
		_ = rows.Close()
		if len(got) != 25 {
			t.Fatalf("scoped read on %s returns %d rows, want 25", table, len(got))
		}
	}

	// 3. PK semantics: the same fact_id under a new scope.
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (fact_id, scope_id, generation_id, fact_kind) VALUES ('proof-fact-0-0', 'proof-scope-new', 'gen-A', 'file')`, whole)); err == nil {
		t.Fatal("undivided PK accepted a duplicate fact_id under a new scope, want rejection")
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (fact_id, scope_id, generation_id, fact_kind) VALUES ('proof-fact-0-0', 'proof-scope-new', 'gen-A', 'file')`, divided)); err != nil {
		t.Fatalf("divided PK rejected a fact_id under a new scope: %v", err)
	}
	exec(fmt.Sprintf(`DELETE FROM %s WHERE fact_id = 'proof-fact-0-0' AND scope_id = 'proof-scope-new'`, divided))
	var collisions int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM (SELECT fact_id FROM %s GROUP BY fact_id HAVING count(DISTINCT scope_id) > 1) c`, divided)).Scan(&collisions); err != nil {
		t.Fatalf("dedup census: %v", err)
	}
	if collisions != 0 {
		t.Fatalf("dedup census = %d, want 0", collisions)
	}

	// 4. fact_id-alone join fans out: every partition appears in the plan.
	joinPlan := planRows(fmt.Sprintf(`EXPLAIN (COSTS OFF) SELECT f.fact_id FROM %s f JOIN %s g ON g.fact_id = f.fact_id WHERE f.fact_kind = 'file' AND g.fact_kind = 'file'`, divided, divided))
	parts := map[string]bool{}
	for _, line := range joinPlan {
		for i := 0; i < 8; i++ {
			if strings.Contains(line, fmt.Sprintf("%s_p%d", divided, i)) {
				parts[fmt.Sprintf("p%d", i)] = true
			}
		}
	}
	if len(parts) != 8 {
		t.Fatalf("unscoped join touches %d partitions, want fan-out over all 8: %v", len(parts), joinPlan)
	}
}
