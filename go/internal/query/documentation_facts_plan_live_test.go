// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	docFactsPlanScope          = "scope:docfacts-plan"
	docFactsPlanKeysetIndex    = "fact_records_scope_generation_keyset_idx"
	docFactsPlanGenerations    = 8
	docFactsPlanRowsPerGen     = 7000
	docFactsPlanMaxBuffers     = 200
	docFactsPlanActiveGenIndex = docFactsPlanGenerations
)

// TestDocumentationFactsActiveScopeReadUsesKeysetIndexLive binds the production
// buildDocumentationFactsSQL statement for a scope_id read (#7128) to a
// representative PostgreSQL plan: eight retained generations of ~7,000 facts
// each, the shape measured on ops-qa. The active-generation bind must let the
// planner walk fact_records_scope_generation_keyset_idx backward in ORDER BY
// order and stop after LIMIT rows, with no Sort node, under both a literal plan
// and a generic plan. The generic plan is the one database/sql clients get, and
// bare EXPLAIN never shows it (#6154 hid a 580 ms generic plan behind a 4.7 ms
// literal EXPLAIN). It is opt-in because it creates a disposable database.
func TestDocumentationFactsActiveScopeReadUsesKeysetIndexLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		6*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedDocumentationFactsPlanProof(t, ctx, db)

	for _, tc := range []struct {
		name string
		// keyset is true when the read must walk the keyset index backward in
		// ORDER BY order. A fact_kind equality lets the planner pick the
		// (scope, generation, fact_kind, observed_at) index and finish the
		// order with an Incremental Sort, which is also bounded by LIMIT; only
		// a full Sort node (a sort of the whole generation) is a regression.
		keyset bool
		filter documentationFactFilter
	}{
		{"default limit 6", true, documentationFactFilter{ScopeID: docFactsPlanScope, Limit: 6}},
		{"default limit 30", true, documentationFactFilter{ScopeID: docFactsPlanScope, Limit: 30}},
		{"default deep offset", true, documentationFactFilter{ScopeID: docFactsPlanScope, Limit: 15, Offset: 500}},
		{"section filter", false, documentationFactFilter{ScopeID: docFactsPlanScope, FactKind: "documentation_section", Limit: 15}},
		{"scoped token", true, documentationFactFilter{ScopeID: docFactsPlanScope, Limit: 15, AllowedScopeIDs: []string{docFactsPlanScope}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, args := buildDocumentationFactsSQL(tc.filter)
			literal := explainDocumentationFactsLiteral(t, ctx, db, query, args)
			assertDocumentationFactsBoundedPlan(t, tc.name+" literal", literal, tc.keyset)
			generic := explainDocumentationFactsGeneric(t, ctx, db, query, args)
			assertDocumentationFactsBoundedPlan(t, tc.name+" generic", generic, tc.keyset)
			// The generic plan must carry the generation bind in an index
			// condition, not as a post-scan filter.
			if !strings.Contains(strings.Join(generic.indexConds(), " "), "generation_id") {
				t.Fatalf("%s generic plan lacks a generation_id index condition: %v", tc.name, generic.indexConds())
			}
		})
	}
}

type docFactsPlanNode struct {
	NodeType      string             `json:"Node Type"`
	RelationName  string             `json:"Relation Name"`
	IndexName     string             `json:"Index Name"`
	ScanDirection string             `json:"Scan Direction"`
	IndexCond     string             `json:"Index Cond"`
	SharedHit     int64              `json:"Shared Hit Blocks"`
	SharedRead    int64              `json:"Shared Read Blocks"`
	Plans         []docFactsPlanNode `json:"Plans"`
}

type docFactsPlan struct {
	Plan          docFactsPlanNode `json:"Plan"`
	ExecutionTime float64          `json:"Execution Time"`
	raw           string
}

func (p docFactsPlan) walk(visit func(docFactsPlanNode)) {
	var rec func(docFactsPlanNode)
	rec = func(n docFactsPlanNode) {
		visit(n)
		for _, c := range n.Plans {
			rec(c)
		}
	}
	rec(p.Plan)
}

func (p docFactsPlan) indexConds() []string {
	var conds []string
	p.walk(func(n docFactsPlanNode) {
		if n.IndexName != "" {
			conds = append(conds, n.IndexCond)
		}
	})
	return conds
}

func assertDocumentationFactsBoundedPlan(t *testing.T, label string, plan docFactsPlan, keyset bool) {
	t.Helper()
	var backward, sorted bool
	plan.walk(func(n docFactsPlanNode) {
		if n.IndexName == docFactsPlanKeysetIndex && n.ScanDirection == "Backward" {
			backward = true
		}
		if n.NodeType == "Sort" {
			sorted = true
		}
	})
	buffers := plan.Plan.SharedHit + plan.Plan.SharedRead
	t.Logf("DOCFACTS_PLAN case=%q backward_keyset=%v sort=%v exec_ms=%.3f buffers=%d", label, backward, sorted, plan.ExecutionTime, buffers)
	if keyset && !backward {
		t.Fatalf("%s: plan did not walk %s backward", label, docFactsPlanKeysetIndex)
	}
	if sorted {
		t.Fatalf("%s: plan sorts the whole generation; ORDER BY must be satisfied by index order", label)
	}
	if buffers > docFactsPlanMaxBuffers {
		t.Fatalf("%s: plan touched %d buffers, want at most %d (early stop after LIMIT)", label, buffers, docFactsPlanMaxBuffers)
	}
}

func explainDocumentationFactsLiteral(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any) docFactsPlan {
	t.Helper()
	literal, err := renderDocumentationProofArgsForTest(query, args)
	if err != nil {
		t.Fatalf("render literal statement: %v", err)
	}
	var raw []byte
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+literal).Scan(&raw); err != nil {
		t.Fatalf("explain literal statement: %v", err)
	}
	return decodeDocumentationFactsPlan(t, raw)
}

// explainDocumentationFactsGeneric plans the production statement as a
// prepared statement forced onto its generic plan.
func explainDocumentationFactsGeneric(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any) docFactsPlan {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer func() {
		// The connection returns to the pool: drop this test's session state so
		// the next case starts clean.
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(cleanup, "DEALLOCATE docfacts_generic")
		_, _ = conn.ExecContext(cleanup, "RESET plan_cache_mode")
		_ = conn.Close()
	}()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force generic plan: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PREPARE docfacts_generic AS "+query); err != nil {
		t.Fatalf("prepare production statement: %v", err)
	}
	literals := make([]string, 0, len(args))
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			literals = append(literals, "'"+strings.ReplaceAll(v, "'", "''")+"'")
		case int:
			literals = append(literals, strconv.Itoa(v))
		default:
			t.Fatalf("unsupported argument type %T", arg)
		}
	}
	var raw []byte
	err = conn.QueryRowContext(ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE docfacts_generic("+strings.Join(literals, ", ")+")").Scan(&raw)
	if err != nil {
		t.Fatalf("explain generic execution: %v", err)
	}
	return decodeDocumentationFactsPlan(t, raw)
}

func decodeDocumentationFactsPlan(t *testing.T, raw []byte) docFactsPlan {
	t.Helper()
	var plans []docFactsPlan
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: err=%v raw=%s", err, raw)
	}
	plans[0].raw = string(raw)
	return plans[0]
}

// seedDocumentationFactsPlanProof seeds one scope with eight retained
// generations (seven superseded, one active), each holding ~7,000 documentation
// facts whose observed_at collides in groups, then ANALYZEs.
func seedDocumentationFactsPlanProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status
) VALUES ($1, 'documentation_source', 'proof', 'proof', 'proof', 'proof',
  clock_timestamp(), clock_timestamp(), 'active')`, docFactsPlanScope); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
)
SELECT 'generation:docfacts-plan-' || g, $1, 'proof',
  TIMESTAMPTZ '2026-09-01 00:00:00+00' + (g * interval '1 day'),
  TIMESTAMPTZ '2026-09-01 00:00:00+00' + (g * interval '1 day'),
  CASE WHEN g = $2 THEN 'active' ELSE 'superseded' END,
  TIMESTAMPTZ '2026-09-01 00:00:00+00' + (g * interval '1 day')
FROM generate_series(1, $2) AS g`, docFactsPlanScope, docFactsPlanGenerations); err != nil {
		t.Fatalf("seed generations: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		fmt.Sprintf("generation:docfacts-plan-%d", docFactsPlanActiveGenIndex), docFactsPlanScope); err != nil {
		t.Fatalf("activate generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'fact:docfacts-plan:' || g || ':' || lpad(n::text, 6, '0'),
  $1, 'generation:docfacts-plan-' || g,
  CASE n % 4 WHEN 0 THEN 'documentation_section' WHEN 1 THEN 'documentation_document'
             WHEN 2 THEN 'documentation_link' ELSE 'documentation_entity_mention' END,
  'stable:' || g || ':' || n,
  'proof', 'proof', 'key:' || g || ':' || n,
  TIMESTAMPTZ '2026-09-01 00:00:00+00' + (g * interval '1 day') + ((n / 7) * interval '1 second'),
  clock_timestamp(),
  jsonb_build_object('document_id', 'doc:' || (n % 500))
FROM generate_series(1, $2) AS g
CROSS JOIN generate_series(1, $3) AS n`,
		docFactsPlanScope, docFactsPlanGenerations, docFactsPlanRowsPerGen); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records; ANALYZE ingestion_scopes; ANALYZE scope_generations"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

// TestDocumentationFactsAnchorOnlyReadsBindActiveGenerationLive proves the two
// anchor-only binding forms (#7128) plan as intended at the ops-qa source
// scale: 1,500 scopes of eight generations each, one documentation_source fact
// per generation (12,000 rows, one row in eight active).
//
//   - active_probe (fact_kind=source alone) keeps the ordered scan of the
//     documentation_source partial index with no Sort, and probes
//     ingestion_scopes by primary key per row;
//   - active_join (a selective anchor, and any scoped token) binds through one
//     INNER JOIN on ingestion_scopes.active_generation_id.
//
// Both are checked under a literal plan and under force_generic_plan.
func TestDocumentationFactsAnchorOnlyReadsBindActiveGenerationLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		6*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedDocumentationFactsSourceScale(t, ctx, db)

	t.Run("probe form keeps the ordered partial-index scan", func(t *testing.T) {
		filter := documentationFactFilter{FactKind: "documentation_source", Limit: 10}
		if got := documentationFactBindingForm(filter); got != documentationFactBindingActiveProbe {
			t.Fatalf("binding form = %q, want active_probe", got)
		}
		query, args := buildDocumentationFactsSQL(filter)
		for label, plan := range map[string]docFactsPlan{
			"literal": explainDocumentationFactsLiteral(t, ctx, db, query, args),
			"generic": explainDocumentationFactsGeneric(t, ctx, db, query, args),
		} {
			var sourcesIdx, sorted, scopePK bool
			plan.walk(func(n docFactsPlanNode) {
				if n.IndexName == "fact_records_documentation_sources_observed_idx" {
					sourcesIdx = true
				}
				if n.NodeType == "Sort" {
					sorted = true
				}
				if n.RelationName == "ingestion_scopes" && strings.Contains(n.NodeType, "Index") {
					scopePK = true
				}
			})
			buffers := plan.Plan.SharedHit + plan.Plan.SharedRead
			t.Logf("DOCFACTS_PLAN form=active_probe mode=%s sources_idx=%v scope_pk_probe=%v sort=%v exec_ms=%.3f buffers=%d",
				label, sourcesIdx, scopePK, sorted, plan.ExecutionTime, buffers)
			if !sourcesIdx || sorted || !scopePK {
				t.Fatalf("%s plan lost the ordered early stop: sources_idx=%v sort=%v scope_pk_probe=%v\n%s",
					label, sourcesIdx, sorted, scopePK, plan.raw)
			}
			if buffers > 600 {
				t.Fatalf("%s plan touched %d buffers, want at most 600", label, buffers)
			}
		}
	})

	for _, tc := range []struct {
		name   string
		filter documentationFactFilter
	}{
		{"selective anchor", documentationFactFilter{FactKind: "documentation_source", SourceID: "doc-source:scale:0042", Limit: 10}},
		{"scoped token", documentationFactFilter{FactKind: "documentation_source", Limit: 10, AllowedScopeIDs: []string{"scope:docfacts-scale-0042"}}},
	} {
		t.Run("join form "+tc.name, func(t *testing.T) {
			if got := documentationFactBindingForm(tc.filter); got != documentationFactBindingActiveJoin {
				t.Fatalf("binding form = %q, want active_join", got)
			}
			query, args := buildDocumentationFactsSQL(tc.filter)
			for label, plan := range map[string]docFactsPlan{
				"literal": explainDocumentationFactsLiteral(t, ctx, db, query, args),
				"generic": explainDocumentationFactsGeneric(t, ctx, db, query, args),
			} {
				var joined bool
				plan.walk(func(n docFactsPlanNode) {
					if n.RelationName == "ingestion_scopes" {
						joined = true
					}
				})
				t.Logf("DOCFACTS_PLAN form=active_join case=%q mode=%s ingestion_scopes=%v exec_ms=%.3f buffers=%d",
					tc.name, label, joined, plan.ExecutionTime, plan.Plan.SharedHit+plan.Plan.SharedRead)
				if !joined || !strings.Contains(plan.raw, "active_generation_id") {
					t.Fatalf("%s plan does not bind through ingestion_scopes.active_generation_id:\n%s", label, plan.raw)
				}
			}
			// The join returns exactly the active generation's row.
			got := documentationFactPayloadIDs(t, ctx, db, query, args...)
			if len(got) != 1 || !strings.HasSuffix(got[0], ":8") {
				t.Fatalf("join-form rows = %v, want the single active-generation fact", got)
			}
		})
	}
}

// seedDocumentationFactsSourceScale seeds 1,500 scopes x eight generations with
// one documentation_source fact per generation; generation 8 is active.
func seedDocumentationFactsSourceScale(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
)
SELECT 'scope:docfacts-scale-' || lpad(s::text, 4, '0'), 'documentation_source', 'proof', 'proof', 'proof', 'proof',
  clock_timestamp(), clock_timestamp(), 'active',
  jsonb_build_object('repo', 'scope:docfacts-scale-' || lpad(s::text, 4, '0'))
FROM generate_series(1, 1500) AS s;
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
)
SELECT 'generation:docfacts-scale-' || lpad(s::text, 4, '0') || ':' || g,
  'scope:docfacts-scale-' || lpad(s::text, 4, '0'), 'proof',
  clock_timestamp(), clock_timestamp(), CASE WHEN g = 8 THEN 'active' ELSE 'superseded' END, clock_timestamp()
FROM generate_series(1, 1500) AS s CROSS JOIN generate_series(1, 8) AS g;
UPDATE ingestion_scopes SET active_generation_id =
  'generation:docfacts-scale-' || substr(scope_id, length('scope:docfacts-scale-') + 1) || ':8'
WHERE scope_id LIKE 'scope:docfacts-scale-%';
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT 'fact:docfacts-scale-' || lpad(s::text, 4, '0') || ':' || g,
  'scope:docfacts-scale-' || lpad(s::text, 4, '0'),
  'generation:docfacts-scale-' || lpad(s::text, 4, '0') || ':' || g,
  'documentation_source', 'stable:' || s || ':' || g, 'proof', 'proof', 'key:' || s || ':' || g,
  TIMESTAMPTZ '2026-09-01 00:00:00+00' + (g * interval '1 day') + (s * interval '1 second'),
  clock_timestamp(),
  jsonb_build_object('source_id', 'doc-source:scale:' || lpad(s::text, 4, '0'))
FROM generate_series(1, 1500) AS s CROSS JOIN generate_series(1, 8) AS g;
ANALYZE fact_records; ANALYZE ingestion_scopes; ANALYZE scope_generations;
`); err != nil {
		t.Fatalf("seed source scale: %v", err)
	}
}
