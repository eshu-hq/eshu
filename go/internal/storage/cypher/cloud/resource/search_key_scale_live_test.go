// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Measurement for #7245 PR-1 on the pinned Neo4j, run only when
// ESHU_INFRA_SEARCH_KEY_SCALE=1 (see search_key_live_test.go for the
// container). It seeds the infra label shape measured in the QA environment
// without keys, times the backfill with infra_search_key_ft present, and
// measures the writer clause
// overhead (wall time and PROFILE db hits) on 5,000 TerraformVariable entity
// rows. Timings are logged, never asserted: the host is shared.
package resource

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// qaLabelShape is the label cardinality the #7245 claim measured in
// the QA environment: three large Terraform labels, 180 CloudResource nodes, and 2,220
// nodes on each other infra label.
func qaLabelShape() map[string]int {
	shape := map[string]int{}
	for _, label := range graph.InfraSearchKeyLabels() {
		shape[label] = 2220
	}
	shape["TerraformVariable"] = 65257
	shape["TerraformResource"] = 35018
	shape["TerraformLocal"] = 30414
	shape["CloudResource"] = 180
	return shape
}

func TestLiveInfraSearchKeyBackfillScale(t *testing.T) {
	if os.Getenv("ESHU_INFRA_SEARCH_KEY_SCALE") != "1" {
		t.Skip("set ESHU_INFRA_SEARCH_KEY_SCALE=1 to run the backfill and write-overhead measurement")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	g := openLiveSearchKeyGraph(ctx, t)
	// The full production Neo4j schema, so writer MATCH and MERGE anchors
	// seek their uid constraints exactly as in production.
	if err := graph.EnsureSchemaWithBackendStrict(ctx, liveSchemaExecutor{g: g}, nil, graph.SchemaBackendNeo4j); err != nil {
		t.Fatalf("EnsureSchemaWithBackendStrict(neo4j) error = %v", err)
	}
	g.ensureIndex(ctx, t)
	const prefix = liveSearchKeyPrefix + "scale:"
	drop := func() {
		g.must(context.Background(), t, "MATCH (n) WHERE n.uid STARTS WITH $p CALL (n) { DETACH DELETE n } IN TRANSACTIONS OF 10000 ROWS", map[string]any{"p": prefix})
	}
	drop()
	defer drop()
	defer g.cleanup(context.Background(), t)

	total := 0
	seedStart := time.Now()
	for label, n := range qaLabelShape() {
		total += n
		g.must(ctx, t, "UNWIND range(1, $n) AS i CALL (i) { CREATE (:"+label+" {uid: $p + $label + ':' + toString(i), id: $p + $label + ':' + toString(i), "+
			"name: 'res-' + toString(i) + '-' + $label, kind: 'kind-' + toString(i % 17), resource_type: 'aws_type_' + toString(i % 31), "+
			"region: 'us-east-1', source: 'terraform', config_path: '/repo/modules/' + toString(i % 97) + '/main.tf'}) } IN TRANSACTIONS OF 5000 ROWS",
			map[string]any{"n": n, "p": prefix, "label": label})
	}
	t.Logf("seeded %d unkeyed infra nodes in %s", total, time.Since(seedStart))
	g.must(ctx, t, "CALL db.awaitIndexes(600)", nil)

	start := time.Now()
	results, err := search.Backfill(ctx, g, search.BackfillOptions{})
	if err != nil {
		t.Fatalf("Backfill() error = %v", err)
	}
	var keyed int64
	for _, r := range results {
		keyed += r.Keyed
		if r.Keyed >= 30000 {
			t.Logf("backfill label=%s keyed=%d duration=%s", r.Label, r.Keyed, r.Duration)
		}
	}
	t.Logf("backfill keyed %d nodes in %s with %s present", keyed, time.Since(start), graph.InfraSearchKeyIndexName)
	start = time.Now()
	if _, err := search.Backfill(ctx, g, search.BackfillOptions{}); err != nil {
		t.Fatalf("second Backfill() error = %v", err)
	}
	t.Logf("idempotent re-run (nothing to write) took %s", time.Since(start))
	if state := g.indexState(ctx, t); state != "ONLINE" {
		t.Fatalf("index state = %s after the backfill, want ONLINE", state)
	}

	// Write overhead: 5,000 TerraformVariable entity rows through the real
	// canonical writer, keyed (Neo4j) against unkeyed (NornicDB shape), three
	// alternating reps on fresh uids, with the FULLTEXT index present.
	entityMat := func(tag string) canonical.CanonicalMaterialization {
		mat := canonical.CanonicalMaterialization{
			ScopeID: prefix + "scope", GenerationID: prefix + "gen-" + tag, RepoID: prefix + "repo",
			Repository: &canonical.RepositoryRow{RepoID: prefix + "repo", Name: "zz7245", Path: "/zz7245/scale"},
			Files:      []canonical.FileRow{{Path: "/zz7245/scale/" + tag + "/vars.tf", RelativePath: tag + "/vars.tf", Name: "vars.tf", Language: "hcl", RepoID: prefix + "repo", DirPath: "/zz7245/scale/" + tag}},
		}
		for i := 0; i < 5000; i++ {
			mat.Entities = append(mat.Entities, canonical.EntityRow{
				EntityID: fmt.Sprintf("%sw-%s-%d", prefix, tag, i), Label: "TerraformVariable", EntityName: fmt.Sprintf("var_%d", i),
				FilePath: "/zz7245/scale/" + tag + "/vars.tf", RelativePath: tag + "/vars.tf", StartLine: i + 1, EndLine: i + 1, Language: "hcl", RepoID: prefix + "repo",
			})
		}
		return mat
	}
	exec := &sourcecypher.InstrumentedExecutor{Inner: g}
	for rep := 0; rep < 3; rep++ {
		for _, d := range []search.Dialect{search.On, search.Off} {
			tag := fmt.Sprintf("%s-%d", d, rep)
			start := time.Now()
			if err := sourcecypher.NewCanonicalNodeWriterWithDialect(exec, 0, nil, d).Write(ctx, entityMat(tag)); err != nil {
				t.Fatalf("Write(%s) error = %v", tag, err)
			}
			t.Logf("write 5000 TerraformVariable rows dialect=%s rep=%d duration=%s", d, rep, time.Since(start))
		}
	}

	// PROFILE db hits for one 500-row entity batch, keyed and unkeyed, on
	// fresh uids so both create the same nodes.
	rows := make([]map[string]any, 0, 500)
	for i := 0; i < 500; i++ {
		rows = append(rows, map[string]any{"entity_id": fmt.Sprintf("%sp-%d", prefix, i), "props": map[string]any{
			"name": fmt.Sprintf("var_%d", i), "id": fmt.Sprintf("%sp-%d", prefix, i), "path": "/zz7245/scale/vars.tf",
		}})
	}
	base := fmt.Sprintf(node.EntityUpsertTemplate, "TerraformVariable")
	for _, d := range []search.Dialect{search.Off, search.On} {
		g.must(ctx, t, "MATCH (n:TerraformVariable) WHERE n.uid STARTS WITH $p DETACH DELETE n", map[string]any{"p": prefix + "p-"})
		hits, elapsed := g.profileDBHits(ctx, t, "PROFILE "+search.Statement(d, "TerraformVariable", "n", base), map[string]any{"rows": rows})
		t.Logf("PROFILE 500-row TerraformVariable upsert dialect=%s db_hits=%d elapsed=%s", d, hits, elapsed)
	}
}

// profileDBHits runs a PROFILE statement and sums db hits over the plan.
func (g *liveSearchKeyGraph) profileDBHits(ctx context.Context, t *testing.T, cypher string, params map[string]any) (int64, time.Duration) {
	t.Helper()
	s := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = s.Close(ctx) }()
	start := time.Now()
	res, err := s.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("%v\ncypher=%s", err, strings.SplitN(cypher, "\n", 2)[0])
	}
	summary, err := res.Consume(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sum func(p neo4jdriver.ProfiledPlan) int64
	sum = func(p neo4jdriver.ProfiledPlan) int64 {
		total := p.DbHits()
		for _, c := range p.Children() {
			total += sum(c)
		}
		return total
	}
	if summary.Profile() == nil {
		t.Fatal("PROFILE returned no plan")
	}
	return sum(summary.Profile()), time.Since(start)
}

// liveSchemaExecutor applies schema DDL through the live session.
type liveSchemaExecutor struct{ g *liveSearchKeyGraph }

func (e liveSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := e.g.Run(ctx, stmt.Cypher, stmt.Parameters)
	return err
}
