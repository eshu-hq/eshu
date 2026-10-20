// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// TestRetirementGraphRetractLive is the #8659 acceptance proof for step 2c
// against a live backend: after the G-statements run, no projector-owned
// node carries the retired repo id (the exact P10 form), the Repository
// node is demoted with the other repository's incoming edge preserved, and
// the live repository's nodes are untouched.
//
// The G1 to G4 edge retracts and the G5 node retracts reuse the existing
// writer paths (run here through the writer's own builders with the retired
// sentinel generation); G5f, G5p, and G6 are the new statements under test.
// R3 proves AC4 (Files retract with no Repository node: G5 anchors on it,
// G5f does not). R4/R5 prove AC3 (a root shared with a live Repository
// skips G5p and the live Parameters survive).
func TestRetirementGraphRetractLive(t *testing.T) {
	runner := openBoltTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tag := fmt.Sprintf("r8659-%d", time.Now().UnixNano())
	repoRetired := "repo-" + tag + "-a"
	repoLive := "repo-" + tag + "-b"
	repoNoNode := "repo-" + tag + "-c"
	repoSharedRetired := "repo-" + tag + "-d"
	repoSharedLive := "repo-" + tag + "-e"
	rootA := "/r8659/" + tag + "/a"
	rootB := "/r8659/" + tag + "/b"
	rootShared := "/r8659/" + tag + "/shared"
	// The shared retired root is nested UNDER the live repository's path:
	// two Repository nodes can never carry the same path under the
	// repository_path uniqueness constraint, so the equal-path shape the
	// shared-root read also matches is unit-pinned only (see
	// TestRetirementSharedRootQueryCoversEqualAndParentPaths), while live
	// coverage uses the parent shape the product schema admits.
	rootSharedChild := rootShared + "/child"
	retirementID := "rr_" + tag

	exec := &boltTestExecutor{runner: runner}
	// The retract runs on the product schema: the statements the schema
	// change adds, created idempotently so the test also passes on a fresh
	// backend the schema applier has not touched. repository_path is
	// included so the seed itself runs under the product uniqueness the
	// shared-root shapes must respect.
	// NornicDB never created composite (name, path, ...) uniqueness forms,
	// so the product schema carries parameter_unique on Neo4j only. Skip it
	// there to mirror the product backend split; the tagged seed CREATEs no
	// duplicate Parameter, so no uniqueness guard is load-bearing here.
	isNornicDB := strings.EqualFold(strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")), "nornicdb")
	for _, ddl := range []string{
		`CREATE CONSTRAINT repository_id IF NOT EXISTS FOR (r:Repository) REQUIRE r.id IS UNIQUE`,
		`CREATE CONSTRAINT repository_path IF NOT EXISTS FOR (r:Repository) REQUIRE r.path IS UNIQUE`,
		`CREATE CONSTRAINT parameter_unique IF NOT EXISTS FOR (p:Parameter) REQUIRE (p.name, p.path, p.function_line_number) IS UNIQUE`,
		`CREATE INDEX file_repo_id IF NOT EXISTS FOR (f:File) ON (f.repo_id)`,
		`CREATE INDEX parameter_path IF NOT EXISTS FOR (p:Parameter) ON (p.path)`,
	} {
		if isNornicDB && strings.Contains(ddl, "parameter_unique") {
			continue
		}
		if err := runner.runCypherSingle(ctx, Statement{Cypher: ddl}); err != nil {
			t.Fatalf("ensure schema %q: %v", ddl, err)
		}
	}

	// All paths and ids are concatenated client-side: NornicDB stores a
	// server-side `$param + 'literal'` concat in CREATE unevaluated (the
	// expression text lands in the property), so the seed carries only
	// plain parameters. Neo4j evaluates either form; plain params keep
	// both backends on identical data.
	seed := `CREATE (ra:Repository {id: $repoRetired, path: $rootA, evidence_source: 'projector/canonical', scope_id: 's-a', generation_id: 'g1'})
CREATE (rb:Repository {id: $repoLive, path: $rootB, evidence_source: 'projector/canonical', scope_id: 's-b', generation_id: 'g1'})
CREATE (rd:Repository {id: $repoSharedRetired, path: $rootSharedChild, evidence_source: 'projector/canonical', scope_id: 's-d', generation_id: 'g1'})
CREATE (re:Repository {id: $repoSharedLive, path: $rootShared, evidence_source: 'projector/canonical', scope_id: 's-e', generation_id: 'g1'})
CREATE (fa:File {path: $pathAGo, repo_id: $repoRetired, evidence_source: 'projector/canonical', generation_id: 'g1'})
CREATE (da:Directory {path: $rootA, repo_id: $repoRetired, evidence_source: 'projector/canonical', generation_id: 'g1'})
CREATE (fna:Function {name: 'main', path: $pathAGo, repo_id: $repoRetired, evidence_source: 'projector/canonical', generation_id: 'g1'})
CREATE (pa:Parameter {name: 'x', path: $pathAGo, function_line_number: 1, evidence_source: 'projector/canonical', generation_id: 'g1'})
CREATE (fb:File {path: $pathBGo, repo_id: $repoLive, evidence_source: 'projector/canonical', generation_id: 'g1'})
CREATE (pb:Parameter {name: 'y', path: $pathBGo, function_line_number: 2, evidence_source: 'projector/canonical', generation_id: 'g1'})
CREATE (fc:File {path: $pathOrphan, repo_id: $repoNoNode, evidence_source: 'projector/canonical', generation_id: 'g1'})
CREATE (pd:Parameter {name: 'd', path: $pathDGo, function_line_number: 3, evidence_source: 'projector/canonical', generation_id: 'g1'})
CREATE (pe:Parameter {name: 'e', path: $pathEGo, function_line_number: 4, evidence_source: 'projector/canonical', generation_id: 'g1'})
CREATE (wa:Workload {id: $wlA, repo_id: $repoRetired, evidence_source: 'reducer/workload'})
CREATE (ia:WorkloadInstance {id: $wliA, repo_id: $repoRetired, evidence_source: 'reducer/workload'})
CREATE (ea:Endpoint {id: $epA, repo_id: $repoRetired, evidence_source: 'reducer/workload'})
CREATE (wb:Workload {id: $wlB, repo_id: $repoLive, evidence_source: 'reducer/workload'})
CREATE (rb)-[dep:DEPENDS_ON {evidence_source: 'other/canonical'}]->(ra)
CREATE (ra)-[def:DEFINES {evidence_source: 'reducer/workload'}]->(wa)
CREATE (wa)-[uses:USES {evidence_source: 'reducer/workload-cloud'}]->(ea)`
	if err := runner.runCypherSingle(ctx, Statement{Cypher: seed, Parameters: map[string]any{
		"repoRetired": repoRetired, "repoLive": repoLive, "repoNoNode": repoNoNode,
		"repoSharedRetired": repoSharedRetired, "repoSharedLive": repoSharedLive,
		"rootA": rootA, "rootB": rootB, "rootShared": rootShared, "rootSharedChild": rootSharedChild,
		"pathAGo": rootA + "/a.go", "pathBGo": rootB + "/b.go", "pathOrphan": rootA + "/orphan.go",
		"pathDGo": rootSharedChild + "/d.go", "pathEGo": rootShared + "/e.go",
		"wlA": "wl-a-" + tag, "wliA": "wli-a-" + tag, "epA": "ep-a-" + tag, "wlB": "wl-b-" + tag,
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		// Per-label deletes: a labelless repo_id scan is NornicDB-unreliable
		// (the File-scoped file_repo_id index serves it and silently drops
		// every other label), so leftovers would accumulate run over run.
		for _, label := range []string{"File", "Directory", "Function", "Workload", "WorkloadInstance", "Endpoint"} {
			_ = runner.runCypherSingle(cleanupCtx, Statement{
				Cypher:     fmt.Sprintf(`MATCH (n:%s) WHERE n.repo_id IN $repos DETACH DELETE n`, label),
				Parameters: map[string]any{"repos": []string{repoRetired, repoLive, repoNoNode, repoSharedRetired, repoSharedLive}},
			})
		}
		_ = runner.runCypherSingle(cleanupCtx, Statement{
			Cypher: `MATCH (n:Repository) WHERE n.id IN $repos DETACH DELETE n`,
			Parameters: map[string]any{
				"repos": []string{repoRetired, repoLive, repoNoNode, repoSharedRetired, repoSharedLive},
			},
		})
		_ = runner.runCypherSingle(cleanupCtx, Statement{
			Cypher: `MATCH (n:Parameter) WHERE (n.path STARTS WITH $rootA OR n.path STARTS WITH $rootB OR n.path STARTS WITH $rootShared) DETACH DELETE n`,
			Parameters: map[string]any{
				"rootA": rootA + "/", "rootB": rootB + "/", "rootShared": rootShared + "/",
			},
		})
	})

	retireOne := func(repoID, root string, shared bool) {
		t.Helper()
		// G5 through the writer's own builders with the retired sentinel:
		// every generation differs from it, so the full-refresh retracts
		// match every node of the repo.
		writer := &CanonicalNodeWriter{}
		mat := canonical.CanonicalMaterialization{RepoID: repoID, GenerationID: RetirementSentinelGenerationID(retirementID)}
		for _, stmt := range writer.buildRetractStatements(mat) {
			if err := exec.Execute(ctx, stmt); err != nil {
				t.Fatalf("G5 %s: %v", repoID, err)
			}
		}
		for _, stmt := range writer.buildEntityRetractStatements(mat) {
			if err := exec.Execute(ctx, stmt); err != nil {
				t.Fatalf("G5 entities %s: %v", repoID, err)
			}
		}
		// G5f: repo-anchored, fires with no Repository node (R3).
		g5f, err := BuildRetirementFileRetractStatement(repoID)
		if err != nil {
			t.Fatalf("G5f build %s: %v", repoID, err)
		}
		if err := exec.Execute(ctx, g5f); err != nil {
			t.Fatalf("G5f %s: %v", repoID, err)
		}
		// G5w: the reducer-owned workload cascade, likewise repo-anchored.
		g5w, err := BuildRetirementWorkloadRetractStatements(repoID)
		if err != nil {
			t.Fatalf("G5w build %s: %v", repoID, err)
		}
		for _, stmt := range g5w {
			if err := exec.Execute(ctx, stmt); err != nil {
				t.Fatalf("G5w %s: %v", repoID, err)
			}
		}
		// G5p, skipped with the shared root recorded (R4/R5).
		if !shared {
			g5p, err := BuildRetirementParameterRetractStatement(root, nil)
			if err != nil {
				t.Fatalf("G5p build %s: %v", repoID, err)
			}
			if err := exec.Execute(ctx, g5p); err != nil {
				t.Fatalf("G5p %s: %v", repoID, err)
			}
		}
		// G6: demote, never delete.
		g6, err := BuildRetirementRepositoryDemoteStatement(repoID, retirementID)
		if err != nil {
			t.Fatalf("G6 build %s: %v", repoID, err)
		}
		if err := exec.Execute(ctx, g6); err != nil {
			t.Fatalf("G6 %s: %v", repoID, err)
		}
	}

	// AC3 detection first: the shared-root read must name the live owner
	// before the skip decision.
	sharedCypher, sharedParams := RetirementSharedRootQuery(repoSharedRetired, rootSharedChild)
	sharedRows, err := runner.runCypher(ctx, sharedCypher, sharedParams)
	if err != nil {
		t.Fatalf("shared-root read: %v", err)
	}
	if len(sharedRows) != 1 || sharedRows[0]["repo_id"] != repoSharedLive {
		t.Fatalf("shared-root read = %v, want the live owner %s", sharedRows, repoSharedLive)
	}

	retireOne(repoRetired, rootA, false)
	retireOne(repoNoNode, rootA, false)
	retireOne(repoSharedRetired, rootSharedChild, true)

	// Exact P10 acceptance: no projector-owned node carries the retired id.
	// A bare aggregation always yields one row (count 0 on empty input),
	// so assert the count, never the row absence.
	for _, repoID := range []string{repoRetired, repoNoNode, repoSharedRetired} {
		if got := retirementBoltRepoIDCount(t, ctx, runner, repoID); got != 0 {
			t.Fatalf("P10 node census %s = %d, want 0", repoID, got)
		}
	}
	// The demoted node keeps only the other repository's incoming edge.
	edgeRows, err := runner.runCypher(ctx,
		`MATCH (r:Repository {id: $repo_id})-[x]-() RETURN type(x) AS typ, x.evidence_source AS evidence_source, count(x) AS count`,
		map[string]any{"repo_id": repoRetired})
	if err != nil {
		t.Fatalf("P10 edge census: %v", err)
	}
	if len(edgeRows) != 1 || edgeRows[0]["typ"] != "DEPENDS_ON" || edgeRows[0]["evidence_source"] != "other/canonical" {
		t.Fatalf("P10 edge census = %v, want the single foreign-owned incoming DEPENDS_ON", edgeRows)
	}
	// Demotion state, not deletion.
	demoted, err := runner.runCypher(ctx,
		`MATCH (r:Repository {id: $repo_id}) RETURN r.evidence_source AS evidence_source, r.path AS path, r.generation_id AS generation_id`,
		map[string]any{"repo_id": repoRetired})
	if err != nil {
		t.Fatalf("demote read: %v", err)
	}
	if len(demoted) != 1 || demoted[0]["evidence_source"] != "retirement/repository" || demoted[0]["path"] != nil {
		t.Fatalf("demoted node = %v, want evidence_source retirement/repository with a cleared path", demoted)
	}
	if demoted[0]["generation_id"] != RetirementSentinelGenerationID(retirementID) {
		t.Fatalf("demoted generation = %v, want the retired sentinel", demoted[0]["generation_id"])
	}
	// Anchored census arms agree with P10.
	checks, err := BuildRetirementResidueCensus(repoRetired, rootA, nil, true)
	if err != nil {
		t.Fatalf("census build: %v", err)
	}
	for _, check := range checks {
		got, err := boltCount(ctx, runner, check.Cypher, check.Parameters)
		if err != nil {
			t.Fatalf("census arm %s: %v", check.Name, err)
		}
		if got != 0 {
			t.Fatalf("census arm %s = %d, want 0", check.Name, got)
		}
	}
	// AC4: R3's file is gone although R3 never had a Repository node.
	assertRetirementBoltCount(t, ctx, runner,
		`MATCH (f:File) WHERE f.repo_id = $repo_id RETURN count(f) AS count`,
		map[string]any{"repo_id": repoNoNode}, 0, "AC4 file without Repository node")
	// AC3: the shared root's Parameters survive the recorded skip.
	assertRetirementBoltCount(t, ctx, runner,
		`MATCH (p:Parameter) WHERE p.path STARTS WITH $root RETURN count(p) AS count`,
		map[string]any{"root": rootShared + "/"}, 2, "AC3 shared-root Parameters survive")
	// The live repository is untouched.
	if got := retirementBoltRepoIDCount(t, ctx, runner, repoLive); got != 2 {
		t.Fatalf("live repository File and Workload survive count = %d, want 2", got)
	}
	assertRetirementBoltCount(t, ctx, runner,
		`MATCH (p:Parameter) WHERE p.path STARTS WITH $root RETURN count(p) AS count`,
		map[string]any{"root": rootB + "/"}, 1, "live repository Parameter survives")
}

// retirementBoltRepoIDCount sums the nodes carrying repoID across the
// projector labels. A labelless repo_id scan is NornicDB-unreliable: the
// File-scoped file_repo_id index serves it and silently drops every other
// label (probe: labelless returned File only; after DROP INDEX it returned
// File+Workload). The labeled equality form agrees on both backends, so
// cross-label censuses loop over labels explicitly.
func retirementBoltRepoIDCount(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, repoID string) int64 {
	t.Helper()
	var total int64
	for _, label := range []string{"File", "Directory", "Function", "Parameter", "Workload", "WorkloadInstance", "Endpoint", "Repository"} {
		got, err := boltCount(ctx, runner,
			fmt.Sprintf(`MATCH (n:%s) WHERE n.repo_id = $repo_id RETURN count(n) AS count`, label),
			map[string]any{"repo_id": repoID})
		if err != nil {
			t.Fatalf("repo census %s %s: %v", label, repoID, err)
		}
		total += got
	}
	return total
}

// assertRetirementBoltCount fails the test unless the count query returns
// want. Local to this file: package cypher's shared harness exposes only
// boltCount, and the orphan leaf carries its own assert copy that this
// package must not import.
func assertRetirementBoltCount(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	query string,
	params map[string]any,
	want int64,
	label string,
) {
	t.Helper()
	got, err := boltCount(ctx, runner, query, params)
	if err != nil {
		t.Fatalf("count %s: %v", label, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", label, got, want)
	}
}
