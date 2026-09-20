// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_label_predicate_timing

// Interleaved timing harness behind the #6786 X11 performance table in
// docs/internal/evidence/6786-nornicdb-label-predicates.md. It seeds one
// repository whose one File CONTAINS 20,000 Functions, one node of each of
// the 20 infrastructure labels, a DEFINES Workload and two OVERRIDES, then
// times the pre-fix statement, the shipped statement and each rejected
// alternative, interleaved, with a nonce write before every timed read.
//
// The statement texts are frozen copies of what was timed, not the
// production builders: the "before" texts no longer exist in production, and
// a copy keeps the table reproducible after the production text moves on.
// Correctness of the shipped statements is proven by the
// live_nornicdb_label_predicates tests, not here; the ROWS lines are a
// digest for checking that each shape returned what the table says.
//
// It has its own build tag because it takes minutes. Run it against a fresh
// container per backend:
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:28120 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query -tags live_nornicdb_label_predicate_timing \
//	  -run TestLiveLabelPredicateTiming -count=1 -v -timeout 20m
package query

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	labelTimingFunctions = 20000
	labelTimingRounds    = 7
	labelTimingBatch     = 2000
)

var labelTimingInfraLabels = []string{
	"K8sResource", "TerraformResource", "TerraformModule", "TerraformDataSource",
	"TerraformBackend", "TerraformImport", "TerraformMovedBlock", "TerraformRemovedBlock",
	"TerraformCheck", "TerraformLockProvider", "TerragruntConfig", "TerragruntDependency",
	"ArgoCDApplication", "ArgoCDApplicationSet", "HelmChart", "HelmValues", "KustomizeOverlay",
	"CrossplaneXRD", "CrossplaneComposition", "CloudFormationResource",
}

var labelTimingOverrideLabels = []string{"Function", "Class", "Interface", "Trait", "Struct", "Enum", "Protocol"}

const labelTimingInfraReturn = `
		RETURN labels(infra)[0] AS type, infra.name AS name,
		       infra.kind AS kind, infra.source AS source,
		       infra.terraform_source AS terraform_source,
		       infra.config_path AS config_path,
		       infra.provider AS provider,
		       coalesce(infra.resource_type, infra.data_type, '') AS resource_type,
		       infra.resource_service AS resource_service,
		       infra.resource_category AS resource_category,
		       f.relative_path AS file_path
		ORDER BY type, name
		LIMIT $limit`

// labelTimingInfraStatement renders the infrastructure read: "before" is the
// pre-fix label test in the MATCH's WHERE, "in" the rejected IN labels()
// chain, and "with" the shipped WITH-attached label test.
func labelTimingInfraStatement(mode string) string {
	terms := make([]string, 0, len(labelTimingInfraLabels))
	for _, label := range labelTimingInfraLabels {
		if mode == "in" {
			terms = append(terms, "'"+label+"' IN labels(infra)")
		} else {
			terms = append(terms, "infra:"+label)
		}
	}
	match := "\n\t\tMATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(infra)\n\t\t"
	if mode == "with" {
		match += "WITH f, infra\n\t\t"
	}
	return match + "WHERE " + strings.Join(terms, " OR ") + labelTimingInfraReturn
}

const labelTimingChangeSurfaceBefore = `MATCH path = (start:Repository {id: $target_id})-[*1..2]->(impacted)
WHERE impacted.id <> $target_id AND any(label IN labels(impacted) WHERE label IN ['Repository', 'Workload', 'WorkloadInstance', 'CloudResource', 'TerraformModule', 'DataAsset'])
RETURN impacted.id as id, impacted.name as name, labels(impacted) as labels, impacted.environment as environment,
	impacted.repo_id as repo_id, length(path) as depth, relationships(path) as rels
ORDER BY depth, name, id
LIMIT $limit`

const labelTimingChangeSurfaceWith = `MATCH path = (start:Repository {id: $target_id})-[*1..2]->(impacted)
WHERE impacted.id <> $target_id
WITH path, impacted
WHERE impacted:Repository OR impacted:Workload OR impacted:WorkloadInstance OR impacted:CloudResource OR impacted:TerraformModule OR impacted:DataAsset
RETURN impacted.id as id, impacted.name as name, labels(impacted) as labels, impacted.environment as environment,
	impacted.repo_id as repo_id, length(path) as depth, relationships(path) as rels
ORDER BY depth, name, id
LIMIT $limit`

const labelTimingChangeSurfaceAfter = `MATCH path = (start:Repository {id: $target_id})-[*1..2]->(impacted)
WHERE impacted.id <> $target_id
  AND ('Repository' IN labels(impacted) OR 'Workload' IN labels(impacted) OR 'WorkloadInstance' IN labels(impacted)
    OR 'CloudResource' IN labels(impacted) OR 'TerraformModule' IN labels(impacted) OR 'DataAsset' IN labels(impacted))
RETURN impacted.id as id, impacted.name as name, labels(impacted) as labels, impacted.environment as environment,
	impacted.repo_id as repo_id, length(path) as depth, relationships(path) as rels
ORDER BY depth, name, id
LIMIT $limit`

// labelTimingOverrideStatement renders the OVERRIDES story read with the
// pre-fix any() filter (before) or the shipped IN labels() OR-chain.
func labelTimingOverrideStatement(before bool) string {
	predicate := func(alias string) string {
		if before {
			return "any(label IN labels(" + alias + ") WHERE label IN $override_labels)"
		}
		terms := make([]string, 0, len(labelTimingOverrideLabels))
		for _, label := range labelTimingOverrideLabels {
			terms = append(terms, "'"+label+"' IN labels("+alias+")")
		}
		return "(" + strings.Join(terms, " OR ") + ")"
	}
	return `
		MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(file:File)-[:CONTAINS]->(source)-[rel:OVERRIDES]->(target)
		WHERE ` + predicate("source") + `
		  AND ` + predicate("target") + `
		RETURN 'outgoing' as direction, type(rel) as type, rel.reason as reason,
		       coalesce(source.id, source.uid) as source_id, source.name as source_name, labels(source) as source_labels,
		       coalesce(target.id, target.uid) as target_id, target.name as target_name, labels(target) as target_labels,
		       file.relative_path as file_path
		ORDER BY source.name, target.name, source_id, target_id
		SKIP $offset
		LIMIT $limit`
}

type labelTimingShape struct {
	name   string
	cypher string
	params map[string]any
}

func TestLiveLabelPredicateTiming(t *testing.T) {
	driver := openLabelTimingDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	seedLabelTimingGraph(ctx, t, driver)

	overrideLabels := make([]any, 0, len(labelTimingOverrideLabels))
	for _, label := range labelTimingOverrideLabels {
		overrideLabels = append(overrideLabels, label)
	}
	infraParams := map[string]any{"repo_id": "p:repo", "limit": 5001}
	changeParams := map[string]any{"target_id": "p:repo", "limit": 51}
	shapes := []labelTimingShape{
		{"infra before (label test in MATCH WHERE)", labelTimingInfraStatement("before"), infraParams},
		{"infra rejected (IN labels)", labelTimingInfraStatement("in"), infraParams},
		{"infra after (WITH label test)", labelTimingInfraStatement("with"), infraParams},
		{"change-surface before (any)", labelTimingChangeSurfaceBefore, changeParams},
		{"change-surface after (IN labels)", labelTimingChangeSurfaceAfter, changeParams},
		{"change-surface rejected (WITH label test)", labelTimingChangeSurfaceWith, changeParams},
		{"overrides before (any)", labelTimingOverrideStatement(true), map[string]any{"repo_id": "p:repo", "limit": 51, "offset": 0, "override_labels": overrideLabels}},
		{"overrides after (IN labels)", labelTimingOverrideStatement(false), map[string]any{"repo_id": "p:repo", "limit": 51, "offset": 0}},
	}

	backend := os.Getenv("ESHU_LIVE_GRAPH_BACKEND")
	for _, shape := range shapes { // warm-up, and a row digest per shape
		rows, _ := labelTimingRead(ctx, t, driver, shape)
		t.Logf("ROWS\t%s\t%s\t%s", backend, shape.name, labelTimingDigest(rows))
	}
	durations := make([][]time.Duration, len(shapes))
	counts := make([]int, len(shapes))
	for round := 0; round < labelTimingRounds; round++ {
		for i, shape := range shapes {
			labelTimingWrite(ctx, t, driver, `MERGE (x:PerfNonce {id: 'nonce'}) SET x.v = $v`,
				map[string]any{"v": fmt.Sprintf("%d-%d", round, i)})
			rows, elapsed := labelTimingRead(ctx, t, driver, shape)
			counts[i] = len(rows)
			durations[i] = append(durations[i], elapsed)
		}
	}
	for i, shape := range shapes {
		sorted := append([]time.Duration(nil), durations[i]...)
		sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
		t.Logf("PERF\t%s\t%s\trows=%d\tmedian=%.4fs\tmin=%.4fs\tmax=%.4fs", backend, shape.name, counts[i],
			sorted[len(sorted)/2].Seconds(), sorted[0].Seconds(), sorted[len(sorted)-1].Seconds())
	}
}

// seedLabelTimingGraph writes the timing corpus, one committed statement per
// write: on NornicDB v1.3.3 a single MATCH ... UNWIND ... CREATE writes one
// node, not the batch.
func seedLabelTimingGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()
	labelTimingWrite(ctx, t, driver, `CREATE (:Repository {id: 'p:repo', name: 'p-repo'})`, nil)
	labelTimingWrite(ctx, t, driver, `CREATE (:File {id: 'p:file', name: 'a-file', relative_path: 'main.go'})`, nil)
	labelTimingWrite(ctx, t, driver, `MATCH (r:Repository {id: 'p:repo'}) MATCH (f:File {id: 'p:file'}) CREATE (r)-[:REPO_CONTAINS]->(f)`, nil)
	for start := 0; start < labelTimingFunctions; start += labelTimingBatch {
		rows := make([]map[string]any, 0, labelTimingBatch)
		for i := start; i < start+labelTimingBatch && i < labelTimingFunctions; i++ {
			rows = append(rows, map[string]any{"id": fmt.Sprintf("p:fn-%06d", i), "name": fmt.Sprintf("fn%06d", i)})
		}
		batch := start / labelTimingBatch
		labelTimingWrite(ctx, t, driver, `UNWIND $rows AS row CREATE (:Function {id: row.id, uid: row.id, name: row.name, p_batch: $b})`,
			map[string]any{"rows": rows, "b": batch})
		labelTimingWrite(ctx, t, driver, `MATCH (f:File {id: 'p:file'}) MATCH (fn:Function {p_batch: $b}) CREATE (f)-[:CONTAINS]->(fn)`,
			map[string]any{"b": batch})
	}
	seeded, _ := labelTimingRead(ctx, t, driver, labelTimingShape{
		name:   "seed read-back",
		cypher: `MATCH (:File {id: 'p:file'})-[:CONTAINS]->(fn:Function) WHERE fn.id IS NOT NULL RETURN fn.id`,
	})
	if len(seeded) != labelTimingFunctions {
		t.Fatalf("seed read back %d contained functions with ids, want %d", len(seeded), labelTimingFunctions)
	}
	for i, label := range labelTimingInfraLabels {
		labelTimingWrite(ctx, t, driver, fmt.Sprintf(`CREATE (:%s {id: 'p:infra-%d', name: 'infra-%d'})`, label, i, i), nil)
		labelTimingWrite(ctx, t, driver, fmt.Sprintf(`MATCH (f:File {id: 'p:file'}) MATCH (x:%s {id: 'p:infra-%d'}) CREATE (f)-[:CONTAINS]->(x)`, label, i), nil)
	}
	labelTimingWrite(ctx, t, driver, `CREATE (:Workload {id: 'p:wl', name: 'z-wl'})`, nil)
	labelTimingWrite(ctx, t, driver, `MATCH (r:Repository {id: 'p:repo'}) MATCH (w:Workload {id: 'p:wl'}) CREATE (r)-[:DEFINES]->(w)`, nil)
	labelTimingWrite(ctx, t, driver, `CREATE (:Class {id: 'p:base', uid: 'p:base', name: 'Base'})`, nil)
	labelTimingWrite(ctx, t, driver, `MATCH (fn:Function {id: 'p:fn-000000'}) MATCH (c:Class {id: 'p:base'}) CREATE (fn)-[:OVERRIDES]->(c)`, nil)
	labelTimingWrite(ctx, t, driver, `MATCH (fn:Function {id: 'p:fn-000001'}) MATCH (c:Class {id: 'p:base'}) CREATE (fn)-[:OVERRIDES]->(c)`, nil)
}

// labelTimingRead runs one auto-commit read, the mode the production
// Neo4jReader uses, and returns the first two columns of each row.
func labelTimingRead(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, shape labelTimingShape) ([]string, time.Duration) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead})
	defer func() { _ = session.Close(ctx) }()
	start := time.Now()
	result, err := session.Run(ctx, shape.cypher, shape.params)
	if err != nil {
		t.Fatalf("read %q: %v", shape.name, err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatalf("collect %q: %v", shape.name, err)
	}
	elapsed := time.Since(start)
	rows := make([]string, 0, len(records))
	for _, record := range records {
		first, _ := record.Get(record.Keys[0])
		second := any("")
		if len(record.Keys) > 1 {
			second, _ = record.Get(record.Keys[1])
		}
		rows = append(rows, fmt.Sprint(first, "/", second))
	}
	return rows, elapsed
}

func labelTimingDigest(rows []string) string {
	digest := append([]string(nil), rows...)
	sort.Strings(digest)
	if len(digest) > 25 {
		return fmt.Sprintf("%v ...(%d rows)", digest[:3], len(rows))
	}
	return fmt.Sprint(digest)
}

func labelTimingWrite(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, cypher string, params map[string]any) {
	t.Helper()
	if _, err := neo4jdriver.ExecuteQuery(ctx, driver, cypher, params, neo4jdriver.EagerResultTransformer); err != nil {
		t.Fatalf("seed %q: %v", cypher, err)
	}
}

// labelTimingSchemaExecutor applies Eshu's schema through the driver.
type labelTimingSchemaExecutor struct {
	driver neo4jdriver.DriverWithContext
}

func (e labelTimingSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, e.driver, stmt.Cypher, stmt.Parameters, neo4jdriver.EagerResultTransformer)
	return err
}

// openLabelTimingDriver connects to ESHU_NEO4J_URI and applies the schema for
// ESHU_LIVE_GRAPH_BACKEND, as production bootstrap does.
func openLabelTimingDriver(t *testing.T) neo4jdriver.DriverWithContext {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	backend := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND"))
	if uri == "" || backend == "" {
		t.Fatal("ESHU_NEO4J_URI and ESHU_LIVE_GRAPH_BACKEND (nornicdb|neo4j) are required")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := graph.EnsureSchemaWithBackend(ctx, labelTimingSchemaExecutor{driver: driver}, logger, graph.SchemaBackend(backend)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return driver
}
