// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const documentationTargetRefsIndexName = "fact_records_documentation_target_refs_idx"

// documentationTargetFactsProofShape sizes the scaled proof corpus.
type documentationTargetFactsProofShape struct {
	Scopes      int // ingestion scopes, one active generation each plus history
	Generations int // generations per scope; only the last is active
	PerGen      int // mention/claim facts per generation
	Semantic    int // semantic.documentation_observation facts in total
	TargetHits  int // mention/claim facts referencing the probe target
	SemanticHit int // semantic facts referencing the probe target
	TextBlocks  int // 32-byte hex blocks of filler text per fact; large values TOAST the payload like real documentation claims
}

// TestDocumentationTargetFactsUsesRefsIndexLive binds the production
// target-facts builder to the partial GIN index on a representative corpus
// (200,000 mention/claim facts across many generations plus semantic
// observations). It also proves the theory behind #7126: the pre-change
// statement, whose kind list adds semantic.documentation_observation, is not
// provably covered by the partial index and never uses it. The proof is
// opt-in because it creates a disposable database and executes the real
// bootstrap DDL.
func TestDocumentationTargetFactsUsesRefsIndexLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		6*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedDocumentationTargetFactsCorpus(t, ctx, db, documentationTargetFactsProofShape{
		Scopes: 20, Generations: 5, PerGen: 2000, Semantic: 2000, TargetHits: 30, SemanticHit: 6, TextBlocks: 8,
	})

	filter := documentationFindingFilter{Repository: "repo:probe-target", Limit: 10}
	newSQL, newArgs := buildDocumentationTargetFactsSQL(filter)
	legacySQL, legacyArgs := legacyDocumentationTargetFactsSQL(filter)

	newPlan := explainTargetFacts(t, ctx, db, newSQL, newArgs)
	if !newPlan.indexes[documentationTargetRefsIndexName] {
		t.Fatalf("production target-facts plan did not use %s: indexes=%v", documentationTargetRefsIndexName, newPlan.indexNames())
	}
	legacyPlan := explainTargetFacts(t, ctx, db, legacySQL, legacyArgs)
	if legacyPlan.indexes[documentationTargetRefsIndexName] {
		t.Fatalf("legacy statement unexpectedly used %s; the #7126 theory (semantic kind defeats the partial index) no longer holds", documentationTargetRefsIndexName)
	}
	t.Logf("TARGET_FACTS_PLAN new_indexes=%v new_ms=%.3f legacy_indexes=%v legacy_ms=%.3f",
		newPlan.indexNames(), newPlan.executionMS, legacyPlan.indexNames(), legacyPlan.executionMS)

	assertTargetFactsMatchLegacy(t, ctx, db, filter)
	got := queryTargetFactPayloads(t, ctx, db, newSQL, newArgs)
	if len(got) != 11 { // limit 10 plus the truncation sentinel row
		t.Fatalf("production read returned %d rows, want 11 (limit+1)", len(got))
	}
}

// TestDocumentationTargetFactsScaleProofLive measures the pre-change and split
// statements on a corpus sized by ESHU_TEST_DOCUMENTATION_TARGET_FACTS_ROWS
// (mention/claim facts) with interleaved, alternating-first-mover runs and
// logs medians. It is a measurement harness, skipped unless that variable is
// set, and it also asserts the two statements return identical rows.
func TestDocumentationTargetFactsScaleProofLive(t *testing.T) {
	rows, _ := strconv.Atoi(os.Getenv("ESHU_TEST_DOCUMENTATION_TARGET_FACTS_ROWS"))
	if rows <= 0 {
		t.Skip("set ESHU_TEST_DOCUMENTATION_TARGET_FACTS_ROWS to run the scaled measurement")
	}
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		30*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	const scopes, generations = 100, 6
	seedDocumentationTargetFactsCorpus(t, ctx, db, documentationTargetFactsProofShape{
		Scopes: scopes, Generations: generations, PerGen: rows / (scopes * generations),
		Semantic: rows / 20, TargetHits: 60, SemanticHit: 6, TextBlocks: 150,
	})
	filter := documentationFindingFilter{Repository: "repo:probe-target", Limit: 10}
	assertTargetFactsMatchLegacy(t, ctx, db, filter)
	newSQL, newArgs := buildDocumentationTargetFactsSQL(filter)
	legacySQL, legacyArgs := legacyDocumentationTargetFactsSQL(filter)

	var legacyMS, newMS []float64
	var legacyHit, legacyRead, newHit, newRead int64
	for i := 0; i < 9; i++ {
		run := func(sqlText string, args []any, ms *[]float64, hit, read *int64) {
			p := explainTargetFacts(t, ctx, db, sqlText, args)
			*ms = append(*ms, p.executionMS)
			*hit, *read = p.sharedHit, p.sharedRead
		}
		if i%2 == 0 {
			run(legacySQL, legacyArgs, &legacyMS, &legacyHit, &legacyRead)
			run(newSQL, newArgs, &newMS, &newHit, &newRead)
		} else {
			run(newSQL, newArgs, &newMS, &newHit, &newRead)
			run(legacySQL, legacyArgs, &legacyMS, &legacyHit, &legacyRead)
		}
	}
	// Report the semantic branch on its own: no index covers its kind, so this
	// is its honest cost on a corpus where the kind has rows.
	parts := newDocumentationTargetFactsParts(filter)
	semanticArgs := append(append([]any{}, parts.args...), parts.limit+1)
	semanticSQL := documentationTargetFactsBranch(documentationTargetSemanticKindClause, parts, len(semanticArgs))
	semanticPlan := explainTargetFacts(t, ctx, db, semanticSQL, semanticArgs)
	semanticPlan2 := explainTargetFacts(t, ctx, db, semanticSQL, semanticArgs)
	t.Logf("TARGET_FACTS_SEMANTIC_BRANCH rows=%d first_ms=%.2f second_ms=%.2f indexes=%v buffers=hit:%d/read:%d",
		rows/20, semanticPlan.executionMS, semanticPlan2.executionMS, semanticPlan2.indexNames(), semanticPlan2.sharedHit, semanticPlan2.sharedRead)
	// Candidate follow-up, measured but not shipped: a partial GIN index over the
	// semantic kind, mirroring the existing target-refs index. Without it the
	// semantic branch is bounded only by the scope/generation btree (a full
	// heap scan here, thousands of skip-scan probes on a many-scope deployment).
	execProofStatements(t, ctx, db, []proofStatement{
		{`CREATE INDEX candidate_semantic_target_refs_idx ON fact_records USING GIN (payload jsonb_path_ops)
WHERE fact_kind = 'semantic.documentation_observation' AND is_tombstone = FALSE`, nil},
		{`ANALYZE fact_records`, nil},
	})
	candidate := explainTargetFacts(t, ctx, db, semanticSQL, semanticArgs)
	t.Logf("TARGET_FACTS_SEMANTIC_BRANCH_CANDIDATE_INDEX ms=%.2f indexes=%v buffers=hit:%d/read:%d",
		candidate.executionMS, candidate.indexNames(), candidate.sharedHit, candidate.sharedRead)
	t.Logf("TARGET_FACTS_SCALE rows=%d legacy_median_ms=%.2f legacy_first_ms=%.2f new_median_ms=%.2f new_first_ms=%.2f legacy_buffers=hit:%d/read:%d new_buffers=hit:%d/read:%d",
		rows, median(legacyMS), legacyMS[0], median(newMS), newMS[1], legacyHit, legacyRead, newHit, newRead)
}

// seedDocumentationTargetFactsCorpus builds a multi-generation documentation
// corpus. Ordinary facts reference one of 5,000 other repositories; exactly
// TargetHits mention/claim facts and SemanticHit semantic facts reference
// repo:probe-target.
func seedDocumentationTargetFactsCorpus(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	shape documentationTargetFactsProofShape,
) {
	t.Helper()
	execProofStatements(t, ctx, db, []proofStatement{{`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
SELECT 'scope:proof:' || s, 'repository', 'proof', 'proof:' || s, 'proof', 'proof', clock_timestamp(), clock_timestamp(), 'active'
FROM generate_series(1, $1::int) AS s`, []any{shape.Scopes}}, {`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'generation:proof:' || s || ':' || g, 'scope:proof:' || s, 'proof', clock_timestamp(), clock_timestamp(),
       CASE WHEN g = $2::int THEN 'active' ELSE 'superseded' END, clock_timestamp()
FROM generate_series(1, $1::int) AS s, generate_series(1, $2::int) AS g`, []any{shape.Scopes, shape.Generations}}, {
		`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'proof:' || s || ':' || g || ':' || n, 'scope:proof:' || s, 'generation:proof:' || s || ':' || g,
       CASE WHEN n % 3 = 0 THEN 'documentation_claim_candidate' ELSE 'documentation_entity_mention' END,
       'proof:' || s || ':' || g || ':' || n, 'proof', 'proof', 'proof:' || s || ':' || g || ':' || n,
       timestamptz '2026-01-01 00:00:00+00' + ((s * 7919 + g * 104729 + n) % 5000000) * interval '1 second',
       clock_timestamp(),
       jsonb_build_object(
         'candidate_refs', jsonb_build_array(jsonb_build_object('kind', 'repository', 'id', 'repo:r' || ((s * 31 + g * 17 + n) % 5000))),
         'document_id', 'doc:' || s || ':' || n,
         'text', (SELECT string_agg(md5((s + g + n)::text || k::text), '') FROM generate_series(1, $4::int) AS k))
FROM generate_series(1, $1::int) AS s, generate_series(1, $2::int) AS g, generate_series(1, $3::int) AS n`,
		[]any{shape.Scopes, shape.Generations, shape.PerGen, shape.TextBlocks},
	}})
	execProofStatements(t, ctx, db, []proofStatement{{`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'proof:semantic:' || n, 'scope:proof:' || (1 + n % $1::int), 'generation:proof:' || (1 + n % $1::int) || ':1',
       'semantic.documentation_observation', 'proof:semantic:' || n, 'proof', 'proof', 'proof:semantic:' || n,
       timestamptz '2026-02-01 00:00:00+00' + n * interval '1 second', clock_timestamp(),
       jsonb_build_object('candidate_refs', jsonb_build_array(jsonb_build_object('kind', 'repository',
         'id', CASE WHEN n <= $3::int THEN 'repo:probe-target' ELSE 'repo:r' || (n % 5000) END)))
FROM generate_series(1, $2::int) AS n`, []any{shape.Scopes, shape.Semantic, shape.SemanticHit}}, {`
UPDATE fact_records SET payload = jsonb_build_object(
    'candidate_refs', jsonb_build_array(jsonb_build_object('kind', 'repository', 'id', 'repo:probe-target')),
    'document_id', 'doc:probe')
WHERE fact_id IN (SELECT fact_id FROM fact_records WHERE fact_kind IN
      ('documentation_entity_mention', 'documentation_claim_candidate') ORDER BY fact_id LIMIT $1::int)`, []any{shape.TargetHits}}, {`ANALYZE fact_records`, nil}})
}

// proofStatement is one parameterized statement of a seed script; the driver
// refuses several commands in one prepared statement, so seeds run one by one.
type proofStatement struct {
	sql  string
	args []any
}

func execProofStatements(t *testing.T, ctx context.Context, db *sql.DB, statements []proofStatement) {
	t.Helper()
	for i, st := range statements {
		if _, err := db.ExecContext(ctx, st.sql, st.args...); err != nil {
			t.Fatalf("seed statement %d: %v", i, err)
		}
	}
}

// targetFactsPlan is the parsed subset of an EXPLAIN (ANALYZE, BUFFERS) plan.
type targetFactsPlan struct {
	indexes     map[string]bool
	executionMS float64
	sharedHit   int64
	sharedRead  int64
}

func (p targetFactsPlan) indexNames() []string {
	names := make([]string, 0, len(p.indexes))
	for name := range p.indexes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func explainTargetFacts(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any) targetFactsPlan {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain target facts: %v", err)
	}
	var docs []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &docs); err != nil || len(docs) != 1 {
		t.Fatalf("decode plan: err=%v raw=%s", err, raw)
	}
	plan := targetFactsPlan{indexes: map[string]bool{}, executionMS: executionTimeMS(t, raw)}
	plan.sharedHit, plan.sharedRead = int64(numberField(docs[0].Plan, "Shared Hit Blocks")), int64(numberField(docs[0].Plan, "Shared Read Blocks"))
	collectPlanIndexNames(docs[0].Plan, plan.indexes)
	return plan
}

// executionTimeMS reads "Execution Time" from an EXPLAIN (FORMAT JSON) result.
func executionTimeMS(t *testing.T, raw []byte) float64 {
	t.Helper()
	var docs []struct {
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &docs); err != nil || len(docs) != 1 {
		t.Fatalf("decode plan timing: err=%v raw=%s", err, raw)
	}
	return docs[0].ExecutionTime
}

func numberField(node map[string]any, key string) float64 {
	v, _ := node[key].(float64)
	return v
}

func collectPlanIndexNames(node map[string]any, into map[string]bool) {
	if name, ok := node["Index Name"].(string); ok {
		into[name] = true
	}
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if m, ok := child.(map[string]any); ok {
			collectPlanIndexNames(m, into)
		}
	}
}

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}
