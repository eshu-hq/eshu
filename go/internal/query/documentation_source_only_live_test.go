// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const documentationSourceOnlyIndexName = "fact_records_documentation_source_only_idx"

// sourceOnlyFixtureFact is one seeded documentation fact of the truth table.
// wantCounted names whether the corrected "no structured refs" predicate must
// count it on the active generation.
type sourceOnlyFixtureFact struct {
	id, kind, payload string
	generation        string
	tombstone         bool
	wantCounted       bool
}

// sourceOnlyTruthTable enumerates every shape of the three ref keys. A missing
// or JSON-null key means "no refs" (#7126: the previous three-valued predicate
// excluded any row with an absent key, so every real Confluence fact, none of
// which carries these keys, was reported as not source-only).
func sourceOnlyTruthTable() []sourceOnlyFixtureFact {
	const active, old = "generation:so:active", "generation:so:old"
	return []sourceOnlyFixtureFact{
		{id: "e1", kind: "documentation_document", payload: `{}`, generation: active, wantCounted: true},
		{id: "e2", kind: "documentation_section", payload: `{"candidate_refs":null,"evidence_refs":null,"linked_entities":null}`, generation: active, wantCounted: true},
		{id: "e3", kind: "documentation_link", payload: `{"candidate_refs":[],"evidence_refs":[],"linked_entities":[]}`, generation: active, wantCounted: true},
		{id: "e4", kind: "documentation_source", payload: `{"candidate_refs":{},"evidence_refs":{}}`, generation: active, wantCounted: true},
		{id: "e5", kind: "documentation_document", payload: `{"evidence_refs":"x"}`, generation: active, wantCounted: true},
		{id: "e6", kind: "documentation_document", payload: `{"linked_entities":5}`, generation: active, wantCounted: true},
		{id: "e7", kind: "documentation_document", payload: `{"candidate_refs":[{"kind":"repository","id":"r"}]}`, generation: active},
		{id: "e8", kind: "documentation_document", payload: `{"evidence_refs":[1]}`, generation: active},
		{id: "e9", kind: "documentation_document", payload: `{"linked_entities":[{"entity_type":"repository","entity_id":"r"}]}`, generation: active},
		{id: "e10", kind: "documentation_document", payload: `{}`, generation: active, tombstone: true},
		{id: "e11", kind: "documentation_document", payload: `{}`, generation: old},
		{id: "e12", kind: "documentation_entity_mention", payload: `{}`, generation: active},
	}
}

// TestDocumentationSourceOnlyCountsFactsWithoutRefKeysLive is the #7126 RED
// regression and truth table for the source-only count. It runs the production
// builder against real Postgres, asserts exact total and per-kind counts, and
// requires the count to equal a brute-force evaluation with every index path
// disabled, with and without scope and authorization filters.
func TestDocumentationSourceOnlyCountsFactsWithoutRefKeysLive(t *testing.T) {
	ctx, db := openSourceOnlyProofDatabase(t)
	seedSourceOnlyFixture(t, ctx, db)

	for name, tc := range map[string]struct {
		filter documentationFindingFilter
		total  int
		kinds  map[string]int
	}{
		"scope": {
			filter: documentationFindingFilter{ScopeID: "scope:so:1"},
			total:  6,
			kinds:  map[string]int{"documentation_source": 1, "documentation_document": 3, "documentation_section": 1, "documentation_link": 1},
		},
		"scope with authorization": {
			filter: documentationFindingFilter{ScopeID: "scope:so:1", AllowedScopeIDs: []string{"scope:so:1"}},
			total:  6,
			kinds:  map[string]int{"documentation_source": 1, "documentation_document": 3, "documentation_section": 1, "documentation_link": 1},
		},
		"authorization excludes scope": {
			filter: documentationFindingFilter{ScopeID: "scope:so:1", AllowedScopeIDs: []string{"scope:other"}},
			total:  0,
			kinds:  map[string]int{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			query, args := buildDocumentationSourceOnlySQL(tc.filter)
			got := scanSourceOnlyCounts(t, ctx, db, query, args, false)
			want := sourceOnlyCountsFromKinds(tc.total, tc.kinds)
			if got != want {
				t.Fatalf("source-only counts = %+v, want %+v", got, want)
			}
			if brute := scanSourceOnlyCounts(t, ctx, db, query, args, true); brute != got {
				t.Fatalf("index path %+v differs from brute force %+v", got, brute)
			}
		})
	}
}

// TestDocumentationSourceOnlyUsesPartialIndexLive proves migration 122's
// partial index serves the builder's exact statement in both a custom and a
// generic plan. A bare EXPLAIN never shows the generic plan; the statement is
// PREPAREd with plan_cache_mode=force_generic_plan.
func TestDocumentationSourceOnlyUsesPartialIndexLive(t *testing.T) {
	ctx, db := openSourceOnlyProofDatabase(t)
	seedSourceOnlyFixture(t, ctx, db)
	seedSourceOnlyBulk(t, ctx, db)
	query, args := buildDocumentationSourceOnlySQL(documentationFindingFilter{ScopeID: "scope:so:1"})
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		plan := explainPreparedWithMode(t, ctx, db, query, args, mode)
		if !plan.indexes[documentationSourceOnlyIndexName] {
			t.Fatalf("%s: plan did not use %s: indexes=%v", mode, documentationSourceOnlyIndexName, plan.indexNames())
		}
	}
}

func openSourceOnlyProofDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		6*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	return ctx, db
}

func seedSourceOnlyFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	execProofStatements(t, ctx, db, []proofStatement{
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES ('scope:so:1', 'documentation', 'confluence', 'so:1', 'confluence', 'so', clock_timestamp(), clock_timestamp(),
        'active', 'generation:so:active')`, nil},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('generation:so:active', 'scope:so:1', 'proof', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp()),
       ('generation:so:old', 'scope:so:1', 'proof', clock_timestamp(), clock_timestamp(), 'superseded', clock_timestamp())`, nil},
	})
	for _, f := range sourceOnlyTruthTable() {
		execProofStatements(t, ctx, db, []proofStatement{{
			`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'scope:so:1', $2, $3, $1, 'confluence', 'confluence', $1, clock_timestamp(), clock_timestamp(), $4, $5::jsonb)`,
			[]any{f.id, f.generation, f.kind, f.tombstone, f.payload},
		}})
	}
	execProofStatements(t, ctx, db, []proofStatement{{`ANALYZE fact_records`, nil}})
}

// seedSourceOnlyBulk adds enough unrelated and referenced facts that a seq scan
// of fact_records is not the cheapest plan, so the plan test asserts what the
// planner chooses rather than what it is forced to use.
func seedSourceOnlyBulk(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	execProofStatements(t, ctx, db, []proofStatement{{`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'bulk:' || n, 'scope:so:1', 'generation:so:active',
       CASE WHEN n % 4 = 0 THEN 'documentation_document' ELSE 'content_entity' END,
       'bulk:' || n, 'confluence', 'confluence', 'bulk:' || n, clock_timestamp(), clock_timestamp(),
       CASE WHEN n % 4 = 0 THEN '{"candidate_refs":[{"kind":"repository","id":"r"}]}'::jsonb ELSE '{"text":"x"}'::jsonb END
FROM generate_series(1, 200000) AS n`, nil}, {`ANALYZE fact_records`, nil}})
}

type sourceOnlyCounts struct{ total, source, document, section, link int }

func sourceOnlyCountsFromKinds(total int, kinds map[string]int) sourceOnlyCounts {
	return sourceOnlyCounts{total, kinds["documentation_source"], kinds["documentation_document"], kinds["documentation_section"], kinds["documentation_link"]}
}

// scanSourceOnlyCounts runs the aggregate on one connection; brute forces the
// plan off every index path first so the result is the reference semantics.
func scanSourceOnlyCounts(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any, brute bool) sourceOnlyCounts {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() {
		for _, s := range []string{"enable_indexscan", "enable_bitmapscan", "enable_indexonlyscan"} {
			_, _ = conn.ExecContext(ctx, "RESET "+s) // restore before the pool reuses the session
		}
		_ = conn.Close()
	}()
	if brute {
		for _, s := range []string{"enable_indexscan", "enable_bitmapscan", "enable_indexonlyscan"} {
			if _, err := conn.ExecContext(ctx, "SET "+s+" = off"); err != nil {
				t.Fatalf("set %s: %v", s, err)
			}
		}
	}
	var c sourceOnlyCounts
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&c.total, &c.source, &c.document, &c.section, &c.link); err != nil {
		t.Fatalf("source-only query: %v\n%s", err, query)
	}
	return c
}

// explainPreparedWithMode PREPAREs the statement and explains EXECUTE under a
// forced plan_cache_mode so the generic plan is what is inspected.
func explainPreparedWithMode(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any, mode string) targetFactsPlan {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() {
		// Drop only this proof's statement (DISCARD ALL would also drop the
		// driver's own cached statements) and restore plan_cache_mode.
		_, _ = conn.ExecContext(ctx, "DEALLOCATE proof_stmt")
		_, _ = conn.ExecContext(ctx, "RESET plan_cache_mode")
		_ = conn.Close()
	}()
	for _, s := range []string{"SET plan_cache_mode = " + mode, "PREPARE proof_stmt AS " + query} {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	literals := make([]string, len(args))
	for i, a := range args {
		if valuer, ok := a.(driver.Valuer); ok { // e.g. the array wrapper
			v, err := valuer.Value()
			if err != nil {
				t.Fatalf("render argument %d: %v", i+1, err)
			}
			a = v
		}
		literals[i] = "'" + strings.ReplaceAll(fmt.Sprint(a), "'", "''") + "'"
	}
	exec := "EXECUTE proof_stmt"
	if len(literals) > 0 {
		exec += "(" + strings.Join(literals, ", ") + ")"
	}
	var raw []byte
	if err := conn.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+exec).Scan(&raw); err != nil {
		t.Fatalf("explain %s: %v", mode, err)
	}
	return parseTargetFactsPlan(t, raw)
}
