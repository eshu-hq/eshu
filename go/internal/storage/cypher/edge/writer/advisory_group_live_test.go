// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package writer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
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

// countingBoltExecutor runs statements on the live runner's driver and
// publishes Bolt summary counters the way the production seam does, so
// WriteStats assertions observe real backend counters.
type countingBoltExecutor struct {
	runner *boltRetractTestRunner
}

func (e countingBoltExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	session := e.runner.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: e.runner.databaseName})
	defer func() { _ = session.Close(ctx) }()
	params := map[string]any{}
	for k, v := range stmt.Parameters {
		params[k] = v
	}
	result, err := session.Run(ctx, stmt.Cypher, params)
	if err != nil {
		return err
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return err
	}
	c := summary.Counters()
	sourcecypher.ReportWriteCounts(ctx, stmt.Cypher, stmt.Parameters, sourcecypher.WriteCounters{
		NodesCreated:         int64(c.NodesCreated()),
		NodesDeleted:         int64(c.NodesDeleted()),
		RelationshipsCreated: int64(c.RelationshipsCreated()),
		RelationshipsDeleted: int64(c.RelationshipsDeleted()),
		PropertiesSet:        int64(c.PropertiesSet()),
	})
	return nil
}

// TestBoltAdvisoryGroupReplaySetsNoProperties pins criterion 11 on a real
// backend: reconciling the already-current desired state again creates
// nothing and sets zero properties (the upsert's conditional SETs skip
// unchanged values instead of rewriting them every replay).
func TestBoltAdvisoryGroupReplaySetsNoProperties(t *testing.T) {
	runner, _ := openAdvisoryGroupLive(t)
	w := NewAdvisoryGroupWriter(countingBoltExecutor{runner: runner}, 0)
	ctx := context.Background()
	suffix := fmt.Sprintf("10411-noop-%d", time.Now().UnixNano())
	repo := "repository:" + suffix
	groupID := "CVE-10411-" + suffix
	advisoryGroupLiveCleanup(t, runner, []string{groupID}, []string{repo})
	advisoryGroupLiveSeedRepos(t, runner, repo)

	group := advisorygroup.Group{
		ID:          groupID,
		CVEIDs:      []string{groupID},
		AdvisoryIDs: []string{"GHSA-10411-noop"},
		Edges: []advisorygroup.Edge{
			{RepositoryID: repo, PackageID: "pkg:npm/a", Ecosystem: "npm", PURL: "pkg:npm/a@1"},
		},
	}
	first, err := w.ReconcileAdvisoryGroup(ctx, group)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if first.PropertiesSet == 0 || first.NodesCreated != 1 || first.EdgesCreated != 1 {
		t.Fatalf("first reconcile stats = %+v, want node+edge created with properties set", first)
	}
	second, err := w.ReconcileAdvisoryGroup(ctx, group)
	if err != nil {
		t.Fatalf("replay reconcile: %v", err)
	}
	if second != (advisorygroup.WriteStats{}) {
		t.Fatalf("replay stats = %+v, want all zero: a no-op reconcile must not churn properties", second)
	}
}

// TestBoltAdvisoryGroupParallelConvergence proves criterion 7's second half
// (#10411): disjoint groups reconciled in parallel and duplicate same-group
// reconciles racing on the group's one conflict key all converge to the same
// graph state with no constraint errors and no duplicated edges.
func TestBoltAdvisoryGroupParallelConvergence(t *testing.T) {
	runner, w := openAdvisoryGroupLive(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("10411-par-%d", time.Now().UnixNano())
	repo := "repository:" + suffix
	advisoryGroupLiveSeedRepos(t, runner, repo)

	groups := make([]advisorygroup.Group, 0, 5)
	ids := make([]string, 0, 5)
	for i := range 5 {
		id := fmt.Sprintf("CVE-10411-PAR%d-%s", i, suffix)
		ids = append(ids, id)
		groups = append(groups, advisorygroup.Group{
			ID:          id,
			CVEIDs:      []string{id},
			AdvisoryIDs: []string{fmt.Sprintf("GHSA-10411-par-%d", i)},
			Edges: []advisorygroup.Edge{
				{RepositoryID: repo, PackageID: fmt.Sprintf("pkg:npm/par%d", i), Ecosystem: "npm", PURL: fmt.Sprintf("pkg:npm/par%d@1", i)},
			},
		})
	}
	advisoryGroupLiveCleanup(t, runner, ids, []string{repo})

	var wg sync.WaitGroup
	errs := make(chan error, 15)
	for i, group := range groups {
		n := 1
		if i < 2 {
			n = 2
		}
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := w.ReconcileAdvisoryGroup(ctx, group); err != nil {
					errs <- err
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("parallel reconcile: %v", err)
	}
	for i, id := range ids {
		pairs, _ := advisoryGroupPairs(t, runner, id)
		want := fmt.Sprintf("[%s|pkg:npm/par%d|npm|pkg:npm/par%d@1]", repo, i, i)
		if pairs != want {
			t.Fatalf("group %s pairs = %q, want %q (no duplicates, no loss)", id, pairs, want)
		}
	}
}
