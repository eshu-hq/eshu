// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package writer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
)

// The live proofs below drive the production AdvisoryGroupWriter against
// the pinned Neo4j (#10411). The live-backend CI job runs this file on
// Neo4j only (ESHU_NEO4J_URI, ESHU_LIVE_GRAPH_BACKEND,
// ESHU_LIVE_GRAPH_DATABASE, auth disabled). Locally:
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<port> ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test -race -tags live_nornicdb_answer_truth ./internal/storage/cypher/edge/writer \
//	  -run '^TestBoltAdvisoryGroup' -count=1 -v
//
// Each test applies the production AdvisoryGroup and Repository DDL first,
// so the advisory_group_id unique constraint is what enforces one node per
// group id.

func openAdvisoryGroupLive(t *testing.T) (*boltRetractTestRunner, *AdvisoryGroupWriter) {
	t.Helper()
	runner := openLiveVerifyRunner(t)
	stmts, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("schema statements: %v", err)
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt, ":AdvisoryGroup)") || strings.Contains(stmt, "CONSTRAINT repository_id ") {
			if err := runner.runCypherSingle(context.Background(), sourceStatement(stmt)); err != nil {
				t.Fatalf("apply schema %q: %v", stmt, err)
			}
		}
	}
	return runner, NewAdvisoryGroupWriter(&boltTestExecutor{runner: runner}, 0)
}

func advisoryGroupLiveCleanup(t *testing.T, runner *boltRetractTestRunner, groupIDs, repoIDs []string) {
	t.Helper()
	t.Cleanup(func() {
		if err := boltWriteStatement(context.Background(), runner,
			`MATCH (n) WHERE (n:Repository AND n.id IN $repos) OR (n:AdvisoryGroup AND n.id IN $groups) DETACH DELETE n`,
			map[string]any{"repos": repoIDs, "groups": groupIDs}); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
}

func advisoryGroupLiveSeedRepos(t *testing.T, runner *boltRetractTestRunner, repoIDs ...string) {
	t.Helper()
	for _, id := range repoIDs {
		if err := boltWriteStatement(context.Background(), runner,
			`MERGE (:Repository {id: $id})`, map[string]any{"id": id}); err != nil {
			t.Fatalf("seed repo %s: %v", id, err)
		}
	}
}

// advisoryGroupPairs reads back the (repository_id, package_id) pairs plus
// node membership for one group by direct Cypher.
func advisoryGroupPairs(t *testing.T, runner *boltRetractTestRunner, groupID string) (pairs string, members string) {
	t.Helper()
	ctx := context.Background()
	rows, err := runner.runCypher(ctx,
		`MATCH (g:AdvisoryGroup {id: $id})
		 OPTIONAL MATCH (r:Repository)-[e:AFFECTED_BY]->(g)
		 WITH g, r.id + '|' + e.package_id + '|' + e.ecosystem + '|' + e.purl AS pair ORDER BY pair
		 RETURN g.cve_ids AS cves, g.advisory_ids AS advisories, g.member_count AS members, collect(pair) AS pairs`,
		map[string]any{"id": groupID})
	if err != nil {
		t.Fatalf("readback %s: %v", groupID, err)
	}
	if len(rows) == 0 {
		return "", ""
	}
	return fmt.Sprint(rows[0]["pairs"]), fmt.Sprintf("cves=%v advisories=%v count=%v", rows[0]["cves"], rows[0]["advisories"], rows[0]["members"])
}

// TestBoltAdvisoryGroupReconcileIsIdempotentAndRetracts proves, on a real
// backend (#10411 criteria 1-6 at the writer seam): a reconcile writes the
// thin node plus exactly the desired edges; a replay is stable; removing
// a pair retracts only it; an edge to an absent Repository writes nothing;
// an empty reconcile deletes the node.
func TestBoltAdvisoryGroupReconcileIsIdempotentAndRetracts(t *testing.T) {
	runner, w := openAdvisoryGroupLive(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("10411-live-%d", time.Now().UnixNano())
	repoA, repoB := "repository:"+suffix+"-a", "repository:"+suffix+"-b"
	groupID := "CVE-10411-" + suffix
	advisoryGroupLiveCleanup(t, runner, []string{groupID}, []string{repoA, repoB})
	advisoryGroupLiveSeedRepos(t, runner, repoA, repoB)

	group := advisorygroup.Group{
		ID:          groupID,
		CVEIDs:      []string{groupID},
		AdvisoryIDs: []string{"GHSA-10411-live"},
		Edges: []advisorygroup.Edge{
			{RepositoryID: repoA, PackageID: "pkg:npm/a", Ecosystem: "npm", PURL: "pkg:npm/a@1"},
			{RepositoryID: repoB, PackageID: "pkg:npm/a", Ecosystem: "npm", PURL: "pkg:npm/a@1"},
			{RepositoryID: repoB, PackageID: "pkg:npm/b", Ecosystem: "npm", PURL: "pkg:npm/b@2"},
			// Absent repository: must contribute nothing.
			{RepositoryID: "repository:" + suffix + "-absent", PackageID: "pkg:npm/a", Ecosystem: "npm", PURL: "pkg:npm/a@1"},
		},
	}
	if _, err := w.ReconcileAdvisoryGroup(ctx, group); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	pairs, members := advisoryGroupPairs(t, runner, groupID)
	wantPairs := fmt.Sprintf("[%s|pkg:npm/a|npm|pkg:npm/a@1 %s|pkg:npm/a|npm|pkg:npm/a@1 %s|pkg:npm/b|npm|pkg:npm/b@2]", repoA, repoB, repoB)
	if pairs != wantPairs {
		t.Fatalf("pairs = %s, want %s", pairs, wantPairs)
	}
	if want := fmt.Sprintf("cves=[%s] advisories=[GHSA-10411-live] count=2", groupID); members != want {
		t.Fatalf("members = %s, want %s", members, want)
	}

	// Replay: same desired state, same graph.
	if _, err := w.ReconcileAdvisoryGroup(ctx, group); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if pairs2, _ := advisoryGroupPairs(t, runner, groupID); pairs2 != wantPairs {
		t.Fatalf("after replay pairs = %s, want %s", pairs2, wantPairs)
	}
	nodes, err := boltCount(ctx, runner, `MATCH (g:AdvisoryGroup {id: $id}) RETURN count(g) AS count`, map[string]any{"id": groupID})
	if err != nil || nodes != 1 {
		t.Fatalf("group nodes after replay = %d, %v; want exactly 1", nodes, err)
	}

	// Drop one pair: only it retracts.
	group.Edges = group.Edges[:2]
	if _, err := w.ReconcileAdvisoryGroup(ctx, group); err != nil {
		t.Fatalf("retract reconcile: %v", err)
	}
	wantPairs = fmt.Sprintf("[%s|pkg:npm/a|npm|pkg:npm/a@1 %s|pkg:npm/a|npm|pkg:npm/a@1]", repoA, repoB)
	if pairs3, _ := advisoryGroupPairs(t, runner, groupID); pairs3 != wantPairs {
		t.Fatalf("after retract pairs = %s, want %s", pairs3, wantPairs)
	}

	// Empty reconcile deletes the node and its remaining edges.
	if _, err := w.ReconcileAdvisoryGroup(ctx, advisorygroup.Group{ID: groupID}); err != nil {
		t.Fatalf("empty reconcile: %v", err)
	}
	if pairs4, _ := advisoryGroupPairs(t, runner, groupID); pairs4 != "" {
		t.Fatalf("after empty reconcile pairs = %q, want no node", pairs4)
	}
}
