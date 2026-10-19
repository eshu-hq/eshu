// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// T4 (issue #5385, migration doc section 2 table row T4) live proof on the
// pinned Neo4j: two repositories with colliding workload names, full
// generations materialized through the production CanonicalNodeWriter.
//
//   - Retirement: re-onboarding alpha's path under a new repo_id deletes the
//     retired identity's Workload, WorkloadInstance, and Endpoint nodes
//     (1->0 settled reads per label), the retired Repository, and every edge
//     on those nodes (no orphan edges afterwards).
//   - Sibling scope: beta's nodes, its cross-scope USES and DOCUMENTS edges,
//     and its other SAME_NAME edges survive untouched.
//   - Regeneration: beta re-projects under its own id and keeps everything.
//
// The reducer-owned nodes are seeded by hand: the reducer does not run in
// this test, and the seeds stand in for its materialization (same labels,
// same repo_id/workload_id properties, same edge families).
//
// Gate: ESHU_CYPHER_BOLT_DSN (or ESHU_NEO4J_URI) must point at a Neo4j
// backend with auth disabled; set ESHU_CYPHER_BOLT_DATABASE=neo4j. When the
// DSN is unset the test skips.

const t4CascadePrefix = "t4cascade"

type t4CascadeScope struct {
	repoID, path, workloadID, instanceID, endpointID string
}

func t4CascadeScopes(prefix string) (alpha, beta t4CascadeScope) {
	alpha = t4CascadeScope{
		repoID:     "repository:" + prefix + "-alpha-old",
		path:       "/eshu-test/" + prefix + "-alpha",
		workloadID: "workload:repository:" + prefix + "-alpha-old:api",
		instanceID: "workload-instance:repository:" + prefix + "-alpha-old:api:prod",
		endpointID: "endpoint:repository:" + prefix + "-alpha-old:api:/v1/things",
	}
	beta = t4CascadeScope{
		repoID:     "repository:" + prefix + "-beta",
		path:       "/eshu-test/" + prefix + "-beta",
		workloadID: "workload:repository:" + prefix + "-beta:api",
		instanceID: "workload-instance:repository:" + prefix + "-beta:api:prod",
		endpointID: "endpoint:repository:" + prefix + "-beta:api:/v1/things",
	}
	return alpha, beta
}

// t4CascadeMaterialization is one projector generation of scope's repository,
// mirroring movedBlockMaterialization: Repository row plus one File row.
func t4CascadeMaterialization(scope t4CascadeScope, repoID, generation string, first bool) canonical.CanonicalMaterialization {
	filePath := scope.path + "/main.go"
	return canonical.CanonicalMaterialization{
		ScopeID:         "git-repository-scope:" + repoID,
		GenerationID:    generation,
		RepoID:          repoID,
		RepoPath:        scope.path,
		FirstGeneration: first,
		Repository: &canonical.RepositoryRow{
			RepoID: repoID, Name: scope.path[strings.LastIndex(scope.path, "/")+1:],
			Path: scope.path, LocalPath: scope.path,
		},
		Files: []canonical.FileRow{{
			Path: filePath, RelativePath: "main.go", Name: "main.go",
			Language: "go", RepoID: repoID,
		}},
	}
}

// t4CascadeSeedGraph writes scope's reducer-owned nodes and edges: the
// DEFINES / INSTANCE_OF / EXPOSES_ENDPOINT fan plus one cross-scope USES
// (AWS-style) and one DOCUMENTS edge, the families the regeneration case
// must not lose.
func t4CascadeSeedGraph(ctx context.Context, t *testing.T, runner *boltRetractTestRunner, scope t4CascadeScope) {
	t.Helper()
	seeds := []string{
		`MERGE (r:Repository {id: $repo}) SET r.path = $path
		 MERGE (w:Workload {id: $workload}) SET w.name = 'api', w.repo_id = $repo
		 MERGE (r)-[:DEFINES]->(w)`,
		`MATCH (w:Workload {id: $workload})
		 MERGE (i:WorkloadInstance {id: $instance}) SET i.workload_id = $workload, i.repo_id = $repo, i.environment = 'prod'
		 MERGE (i)-[:INSTANCE_OF]->(w)`,
		`MATCH (r:Repository {id: $repo}), (w:Workload {id: $workload})
		 MERGE (e:Endpoint {id: $endpoint}) SET e.path = '/v1/things', e.repo_id = $repo, e.workload_id = $workload
		 MERGE (r)-[:EXPOSES_ENDPOINT]->(e)
		 MERGE (w)-[:EXPOSES_ENDPOINT]->(e)`,
		`MATCH (i:WorkloadInstance {id: $instance})
		 MERGE (c:ContainerImage {uid: $image}) MERGE (i)-[u:USES]->(c) SET u.evidence_source = 'test/t4'`,
		`MATCH (w:Workload {id: $workload})
		 MERGE (d:Document {uid: $doc}) MERGE (d)-[:DOCUMENTS]->(w)`,
	}
	params := map[string]any{
		"repo": scope.repoID, "path": scope.path, "workload": scope.workloadID,
		"instance": scope.instanceID, "endpoint": scope.endpointID,
		"image": t4CascadePrefix + "-image-" + scope.repoID, "doc": t4CascadePrefix + "-doc-" + scope.repoID,
	}
	for _, cypher := range seeds {
		if err := boltWriteStatement(ctx, runner, cypher, params); err != nil {
			t.Fatalf("seed graph: %v", err)
		}
	}
}

// t4CascadeSettledCount reads count until two consecutive reads agree, so a
// not-yet-settled backend cannot fake a zero (the stale-read trap the
// migration ledger rows record).
func t4CascadeSettledCount(ctx context.Context, t *testing.T, runner *boltRetractTestRunner, cypher string, params map[string]any) int64 {
	t.Helper()
	previous, err := boltCount(ctx, runner, cypher, params)
	if err != nil {
		t.Fatalf("settled count: %v", err)
	}
	for range 10 {
		time.Sleep(200 * time.Millisecond)
		current, err := boltCount(ctx, runner, cypher, params)
		if err != nil {
			t.Fatalf("settled count: %v", err)
		}
		if current == previous {
			return current
		}
		previous = current
	}
	t.Fatalf("count never settled: %s", cypher)
	return -1
}

func t4CascadeScopedCount(label, repo string) (string, map[string]any) {
	return fmt.Sprintf("MATCH (n:%s {repo_id: $repo}) RETURN count(n) AS count", label),
		map[string]any{"repo": repo}
}

// t4CascadeProfile runs PROFILE over one statement and returns the rendered
// operator tree. PROFILE executes the statement: the caller seeds a fixture
// it intends to delete.
func t4CascadeProfile(ctx context.Context, t *testing.T, runner *boltRetractTestRunner, stmt Statement) string {
	t.Helper()
	session := runner.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: runner.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, "PROFILE "+stmt.Cypher, stmt.Parameters)
	if err != nil {
		t.Fatalf("PROFILE run: %v", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		t.Fatalf("PROFILE consume: %v", err)
	}
	var lines []string
	if profiled := summary.Profile(); profiled != nil {
		var walk func(p neo4jdriver.ProfiledPlan, depth int)
		walk = func(p neo4jdriver.ProfiledPlan, depth int) {
			args := ""
			if details, ok := p.Arguments()["Details"]; ok {
				args = fmt.Sprintf(" %v", details)
			}
			lines = append(lines, fmt.Sprintf("%s%s%s [rows=%d dbhits=%d]",
				strings.Repeat("  ", depth), p.Operator(), args, p.Records(), p.DbHits()))
			for _, child := range p.Children() {
				walk(child, depth+1)
			}
		}
		walk(profiled, 0)
	} else if plan := summary.Plan(); plan != nil {
		var walk func(p neo4jdriver.Plan, depth int)
		walk = func(p neo4jdriver.Plan, depth int) {
			args := ""
			if details, ok := p.Arguments()["Details"]; ok {
				args = fmt.Sprintf(" %v", details)
			}
			lines = append(lines, fmt.Sprintf("%s%s%s", strings.Repeat("  ", depth), p.Operator(), args))
			for _, child := range p.Children() {
				walk(child, depth+1)
			}
		}
		walk(plan, 0)
	} else {
		t.Fatal("PROFILE returned neither a profile nor a plan")
	}
	return strings.Join(lines, "\n")
}

func t4CascadeAssertIndexedPlan(t *testing.T, tree, leg string) {
	t.Helper()
	t.Logf("PROFILE plan for the %s leg:\n%s", leg, tree)
	if !strings.Contains(tree, "NodeIndexSeek") {
		t.Fatalf("%s leg plan has no NodeIndexSeek:\n%s", leg, tree)
	}
	for _, banned := range []string{"AllNodesScan", "NodeByLabelScan"} {
		if strings.Contains(tree, banned) {
			t.Fatalf("%s leg plan contains %s:\n%s", leg, banned, tree)
		}
	}
}

// t4CascadeApplySchema applies the production Neo4j schema and waits for
// every index to come ONLINE, so the plan proof observes the deployed shape
// instead of a populating index. The Endpoint leg's NodeIndexSeek needs the
// endpoint_repo_id index: an index-less container cannot prove the deployed
// shape.
func t4CascadeApplySchema(ctx context.Context, t *testing.T, runner *boltRetractTestRunner) {
	t.Helper()
	statements, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("schema statements: %v", err)
	}
	for _, ddl := range statements {
		if err := boltWriteStatement(ctx, runner, ddl, nil); err != nil {
			t.Fatalf("apply schema: %v", err)
		}
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		rows, err := runner.runCypher(ctx,
			"SHOW INDEXES YIELD state WHERE state <> 'ONLINE' RETURN count(*) AS pending", nil)
		if err != nil {
			t.Fatalf("index state: %v", err)
		}
		if len(rows) == 0 {
			return
		}
		if v, ok := rows[0]["pending"].(int64); !ok || v == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("indexes never came ONLINE")
		}
		time.Sleep(time.Second)
	}
}

func TestLiveRepositoryRetirementCascadeT4(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()
	alpha, beta := t4CascadeScopes(t4CascadePrefix)
	alphaNew := "repository:" + t4CascadePrefix + "-alpha-new"

	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (n) WHERE n.path STARTS WITH $root OR n.id STARTS WITH $prefix OR n.uid STARTS WITH $prefix DETACH DELETE n`,
			map[string]any{"root": "/eshu-test/" + t4CascadePrefix, "prefix": t4CascadePrefix})
	}
	cleanup()
	t.Cleanup(cleanup)

	t4CascadeApplySchema(ctx, t, runner)

	exec := &boltTestExecutor{runner: runner}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	// Full generations, colliding workload names.
	if err := writer.Write(ctx, t4CascadeMaterialization(alpha, alpha.repoID, "gen-t4-1", true)); err != nil {
		t.Fatalf("alpha gen-1 Write: %v", err)
	}
	if err := writer.Write(ctx, t4CascadeMaterialization(beta, beta.repoID, "gen-t4-1", true)); err != nil {
		t.Fatalf("beta gen-1 Write: %v", err)
	}
	t4CascadeSeedGraph(ctx, t, runner, alpha)
	t4CascadeSeedGraph(ctx, t, runner, beta)
	if err := boltWriteStatement(ctx, runner,
		`MATCH (a:Workload {id: $a}), (b:Workload {id: $b}) MERGE (a)-[:SAME_NAME]->(b)`,
		map[string]any{"a": alpha.workloadID, "b": beta.workloadID}); err != nil {
		t.Fatalf("SAME_NAME seed: %v", err)
	}

	for _, scope := range []t4CascadeScope{alpha, beta} {
		for _, label := range []string{"Workload", "WorkloadInstance", "Endpoint"} {
			cypher, params := t4CascadeScopedCount(label, scope.repoID)
			if got := t4CascadeSettledCount(ctx, t, runner, cypher, params); got != 1 {
				t.Fatalf("pre %s %s count = %d, want 1", scope.repoID, label, got)
			}
		}
	}

	// Retirement (alpha re-onboarded under a new id) and regeneration
	// (beta under its own id) through the production writer.
	if err := writer.Write(ctx, t4CascadeMaterialization(alpha, alphaNew, "gen-t4-2", false)); err != nil {
		t.Fatalf("alpha re-onboard Write: %v", err)
	}
	if err := writer.Write(ctx, t4CascadeMaterialization(beta, beta.repoID, "gen-t4-2", false)); err != nil {
		t.Fatalf("beta regen Write: %v", err)
	}

	// Retirement: 1->0 settled reads per label on the retired scope.
	for _, label := range []string{"Workload", "WorkloadInstance", "Endpoint"} {
		cypher, params := t4CascadeScopedCount(label, alpha.repoID)
		if got := t4CascadeSettledCount(ctx, t, runner, cypher, params); got != 0 {
			t.Fatalf("retired %s count = %d, want 0", label, got)
		}
	}
	if got := t4CascadeSettledCount(ctx, t, runner,
		`MATCH (r:Repository {id: $repo}) RETURN count(r) AS count`, map[string]any{"repo": alpha.repoID}); got != 0 {
		t.Fatalf("retired Repository count = %d, want 0", got)
	}
	if got := t4CascadeSettledCount(ctx, t, runner,
		`MATCH (r:Repository {id: $repo}) RETURN count(r) AS count`, map[string]any{"repo": alphaNew}); got != 1 {
		t.Fatalf("re-onboarded Repository count = %d, want 1", got)
	}

	// Sibling scope untouched, including its cross-scope edges.
	for _, label := range []string{"Workload", "WorkloadInstance", "Endpoint"} {
		cypher, params := t4CascadeScopedCount(label, beta.repoID)
		if got := t4CascadeSettledCount(ctx, t, runner, cypher, params); got != 1 {
			t.Fatalf("sibling %s count = %d, want 1", label, got)
		}
	}
	for _, edge := range []struct {
		cypher string
		want   int64
	}{
		{`MATCH (:WorkloadInstance {repo_id: $repo})-[r:USES]->(:ContainerImage) RETURN count(r) AS count`, 1},
		{`MATCH (:Document)-[r:DOCUMENTS]->(:Workload {repo_id: $repo}) RETURN count(r) AS count`, 1},
		{`MATCH (:Workload {repo_id: $repo})-[r:SAME_NAME]-() RETURN count(r) AS count`, 0},
	} {
		if got := t4CascadeSettledCount(ctx, t, runner, edge.cypher, map[string]any{"repo": beta.repoID}); got != edge.want {
			t.Fatalf("sibling edge %q = %d, want %d", edge.cypher, got, edge.want)
		}
	}

	// No orphan edges: no relationship touches a retired id afterwards.
	dead := []string{alpha.repoID, alpha.workloadID, alpha.instanceID, alpha.endpointID}
	if got := t4CascadeSettledCount(ctx, t, runner,
		`MATCH (a)-[r]->(b) WHERE a.id IN $dead OR b.id IN $dead RETURN count(r) AS count`,
		map[string]any{"dead": dead}); got != 0 {
		t.Fatalf("relationships touching retired ids = %d, want 0", got)
	}
	if got := t4CascadeSettledCount(ctx, t, runner,
		`MATCH (n) WHERE n.repo_id = $repo RETURN count(n) AS count`, map[string]any{"repo": alpha.repoID}); got != 0 {
		t.Fatalf("nodes under retired repo_id = %d, want 0", got)
	}
}

// TestLiveRepositoryRetirementCascadePlansT4 PROFILEs each cascade template
// on a seeded retired identity: every leg must seek its repo_id index (no
// AllNodesScan, no NodeByLabelScan on the hot path), and the profiled run
// itself deletes exactly the scope's nodes.
func TestLiveRepositoryRetirementCascadePlansT4(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()
	alpha, _ := t4CascadeScopes(t4CascadePrefix + "-plan")

	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (n) WHERE n.path STARTS WITH $root OR n.id STARTS WITH $prefix OR n.uid STARTS WITH $prefix DETACH DELETE n`,
			map[string]any{"root": "/eshu-test/" + t4CascadePrefix + "-plan", "prefix": t4CascadePrefix + "-plan"})
	}
	cleanup()
	t.Cleanup(cleanup)

	t4CascadeApplySchema(ctx, t, runner)

	exec := &boltTestExecutor{runner: runner}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	if err := writer.Write(ctx, t4CascadeMaterialization(alpha, alpha.repoID, "gen-t4p-1", true)); err != nil {
		t.Fatalf("gen-1 Write: %v", err)
	}
	t4CascadeSeedGraph(ctx, t, runner, alpha)

	params := map[string]any{"repo_id": alpha.repoID + "-new", "path": alpha.path}
	legs := []struct {
		label  string
		cypher string
	}{
		{"Workload", node.RepositoryRetirementWorkloadCascadeCypher},
		{"WorkloadInstance", node.RepositoryRetirementWorkloadInstanceCascadeCypher},
		{"Endpoint", node.RepositoryRetirementEndpointCascadeCypher},
	}
	for _, leg := range legs {
		// The retired identity holds the path under a different id: the
		// guard must match. Here the live node IS the retired one, so the
		// re-onboard id differs from the stored one.
		tree := t4CascadeProfile(ctx, t, runner, Statement{
			Operation:  OperationCanonicalRetract,
			Cypher:     leg.cypher,
			Parameters: params,
		})
		t4CascadeAssertIndexedPlan(t, tree, leg.label)
	}
	for _, label := range []string{"Workload", "WorkloadInstance", "Endpoint"} {
		cypher, cparams := t4CascadeScopedCount(label, alpha.repoID)
		if got := t4CascadeSettledCount(ctx, t, runner, cypher, cparams); got != 0 {
			t.Fatalf("profiled %s count = %d, want 0", label, got)
		}
	}
}
