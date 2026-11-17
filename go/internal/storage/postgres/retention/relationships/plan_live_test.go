// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 plan guard: on a populated migrated schema (400k evidence rows, 8,000 live generations plus 100 orphans, VACUUM ANALYZE) the three delete statements read evidence, candidates and generations through their generation index or primary key and never sequentially scan the table they delete from, in the custom plan and in the generic plan a driver that caches prepared statements switches to after five runs (docs/internal/evidence/10823-relationship-retention.md)
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// planNode is one node of an EXPLAIN (FORMAT JSON) plan, flattened.
type planNode struct{ nodeType, relation, index string }

// flattenPlan walks an EXPLAIN (FORMAT JSON) document into its nodes.
func flattenPlan(t *testing.T, raw []byte) []planNode {
	t.Helper()
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v (%d plans)\n%s", err, len(plans), raw)
	}
	var nodes []planNode
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		relation, _ := node["Relation Name"].(string)
		index, _ := node["Index Name"].(string)
		nodes = append(nodes, planNode{nodeType, relation, index})
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				if m, ok := child.(map[string]any); ok {
					walk(m)
				}
			}
		}
	}
	walk(plans[0].Plan)
	return nodes
}

// seedPlanCorpus seeds a corpus the planner has to choose indexes on: 4,000
// scopes with a pointed-at and a superseded generation each (8,000 live
// generations, 40 evidence and 10 candidate rows apiece), plus 100 orphan
// generations of 20 evidence and 6 candidate rows. It returns the ids of the
// first 50 orphans, one batch.
func (f *liveFixture) seedPlanCorpus(t *testing.T) []string {
	t.Helper()
	f.exec(t, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
    observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'scope-'||n, 'repository', 'git', 'key-'||n, 'git', 'p-'||n, now(), now(), 'active', 'live-'||n||'-a', '{}'::jsonb
FROM generate_series(1, 4000) n`)
	f.exec(t, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
SELECT 'live-'||n||'-'||g, 'scope-'||n, 'snapshot', now(), now(), CASE g WHEN 'a' THEN 'active' ELSE 'superseded' END, '{}'::jsonb
FROM generate_series(1, 4000) n, (VALUES ('a'), ('s')) v(g)`)
	f.exec(t, `INSERT INTO relationship_generations (generation_id, scope, run_id, status, created_at, activated_at)
SELECT 'live-'||n||'-'||g, 'scope-'||n, 'run', CASE g WHEN 'a' THEN 'active' ELSE 'superseded' END, now(), now()
FROM generate_series(1, 4000) n, (VALUES ('a'), ('s')) v(g)`)
	f.exec(t, `INSERT INTO relationship_generations (generation_id, scope, run_id, status, created_at)
SELECT 'orph-'||lpad(k::text, 4, '0'), 'run-scope', 'run', 'superseded', now() FROM generate_series(1, 100) k`)
	f.exec(t, `INSERT INTO relationship_evidence_facts (evidence_id, generation_id, evidence_kind, relationship_type, source_repo_id,
    target_repo_id, source_entity_id, target_entity_id, confidence, rationale, details, observed_at)
SELECT g||'-ev-'||i, g, 'terraform', 'DEPENDS_ON', 'repo-'||(i%17), 'repo-t', 'src-'||i, 'tgt-'||i, 0.9,
       'seed rationale text of moderate length for the row', jsonb_build_object('path', 'modules/x/main.tf', 'line', i), now()
FROM (SELECT 'live-'||n||'-'||v.g AS g, 40 AS rows FROM generate_series(1, 4000) n, (VALUES ('a'), ('s')) v(g)
      UNION ALL SELECT 'orph-'||lpad(k::text, 4, '0'), 20 FROM generate_series(1, 100) k) gens,
     generate_series(1, gens.rows) i`)
	f.exec(t, `INSERT INTO relationship_candidates (candidate_id, generation_id, source_repo_id, target_repo_id, source_entity_id,
    target_entity_id, relationship_type, confidence, evidence_count, rationale, details)
SELECT g||'-c-'||i, g, 'repo-'||(i%17), 'repo-t', 'src-'||i, 'tgt-'||i, 'DEPENDS_ON', 0.8, 1, 'seed rationale text', '{}'::jsonb
FROM (SELECT 'live-'||n||'-'||v.g AS g, 10 AS rows FROM generate_series(1, 4000) n, (VALUES ('a'), ('s')) v(g)
      UNION ALL SELECT 'orph-'||lpad(k::text, 4, '0'), 6 FROM generate_series(1, 100) k) gens,
     generate_series(1, gens.rows) i`)
	for _, table := range []string{
		"ingestion_scopes", "scope_generations", "relationship_generations",
		"relationship_evidence_facts", "relationship_candidates",
	} {
		f.exec(t, `VACUUM (ANALYZE) `+table)
	}
	batch := make([]string, 0, 50)
	for k := 1; k <= 50; k++ {
		batch = append(batch, fmt.Sprintf("orph-%04d", k))
	}
	return batch
}

// explainCustom plans the statement with its parameters bound, the custom plan
// a fresh statement gets.
func (f *liveFixture) explainCustom(t *testing.T, statement string, args ...any) []planNode {
	t.Helper()
	var raw []byte
	if err := f.db.QueryRowContext(f.ctx, `EXPLAIN (FORMAT JSON) `+statement, args...).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	return flattenPlan(t, raw)
}

// explainGeneric plans the statement as a prepared statement under
// plan_cache_mode = force_generic_plan, the plan a driver that caches prepared
// statements serves after the fifth run. A bare EXPLAIN never shows it. The
// array is passed as a literal to EXECUTE; the helper checks the plan still
// references $1 so a silently custom plan cannot pass as the generic one.
func (f *liveFixture) explainGeneric(t *testing.T, name, statement string, ids []string) []planNode {
	t.Helper()
	ctx := context.Background()
	conn, err := f.db.Conn(f.ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SET plan_cache_mode = force_generic_plan`); err != nil {
		t.Fatalf("force generic plan: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `RESET plan_cache_mode`) }()
	if _, err := conn.ExecContext(ctx, "PREPARE "+name+"(text[]) AS "+statement); err != nil {
		t.Fatalf("prepare %s: %v", name, err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, "DEALLOCATE "+name) }()
	literal := "'{" + strings.Join(ids, ",") + "}'"
	var raw []byte
	if err := conn.QueryRowContext(f.ctx, "EXPLAIN (FORMAT JSON) EXECUTE "+name+"("+literal+")").Scan(&raw); err != nil {
		t.Fatalf("explain generic %s: %v", name, err)
	}
	if !strings.Contains(string(raw), "$1") {
		t.Fatalf("force_generic_plan produced a custom plan for %s (no $1):\n%s", name, raw)
	}
	return flattenPlan(t, raw)
}

// requireIndexServed fails unless some node reads the index and no node scans
// the table sequentially.
func requireIndexServed(t *testing.T, label string, nodes []planNode, table, index string) {
	t.Helper()
	usesIndex := false
	for _, node := range nodes {
		if node.index == index {
			usesIndex = true
		}
		if strings.HasSuffix(node.nodeType, "Seq Scan") && node.relation == table {
			t.Errorf("%s: %s is read with a %s, want a read through %s\nplan nodes: %v", label, table, node.nodeType, index, nodes)
		}
	}
	if !usesIndex {
		t.Errorf("%s: no node reads %s\nplan nodes: %v", label, index, nodes)
	}
}

// TestRelationshipRetentionStatementsAreIndexServedLive pins the plan shapes
// the sweep's deletes rely on. A delete batch removes about a thousand rows, so
// each delete must read its table through the generation index (evidence and
// candidates, the leading column of both indexes) or the primary key (the
// generation rows) and never scan the table it deletes from: a sequential scan
// would make the batch cost grow with the table instead of with the rows it
// deletes. It is checked for the custom plan and for the generic plan a pooled
// driver serves after five runs.
//
// The census statements are not pinned. They read every generation id once, and
// the planner picks an index only scan or a parallel sequential scan by table
// size and visibility map coverage (the evidence note records both), so a pin
// would fail on a plan that costs the same.
//
// Like the purge's plan flip (docs/internal/evidence/10820-census-2e-purge.md),
// the delete plans hold while a batch is a small share of the table: verified
// on PostgreSQL 18, and this test pins it against a server upgrade or a
// statement edit.
func TestRelationshipRetentionStatementsAreIndexServedLive(t *testing.T) {
	f := openLiveFixture(t)
	batch := f.seedPlanCorpus(t)

	const (
		evidence      = "relationship_evidence_facts"
		evidenceIndex = "relationship_evidence_generation_idx"
		candidates    = "relationship_candidates"
		candIndex     = "relationship_candidates_generation_idx"
		generations   = "relationship_generations"
		generationPK  = "relationship_generations_pkey"
	)
	for _, tc := range []struct {
		name, statement, table, index string
	}{
		{"delete_evidence", relationshipretention.DeleteEvidenceSQL, evidence, evidenceIndex},
		{"delete_candidates", relationshipretention.DeleteCandidatesSQL, candidates, candIndex},
		{"delete_generations", relationshipretention.DeleteGenerationsSQL, generations, generationPK},
	} {
		t.Run(tc.name+" custom plan", func(t *testing.T) {
			requireIndexServed(t, tc.name, f.explainCustom(t, tc.statement, batch), tc.table, tc.index)
		})
		t.Run(tc.name+" generic plan", func(t *testing.T) {
			requireIndexServed(t, tc.name+" (generic)", f.explainGeneric(t, "retention_"+tc.name, tc.statement, batch), tc.table, tc.index)
		})
	}
}
