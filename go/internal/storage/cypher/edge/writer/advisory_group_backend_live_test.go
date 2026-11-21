// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package writer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// openAdvisoryGroupLiveBackend opens the production AdvisoryGroupWriter
// against whichever backend ESHU_NEO4J_URI points at, applying that
// backend's AdvisoryGroup schema. It exists because openAdvisoryGroupLive
// always applies the Neo4j DDL: issue #11161 is a NornicDB-vs-Neo4j
// dialect gap, so the regression must run the writer on the gate-default
// NornicDB image, not only on pinned Neo4j.
func openAdvisoryGroupLiveBackend(t *testing.T) (*boltRetractTestRunner, *AdvisoryGroupWriter) {
	t.Helper()
	runner := openLiveVerifyRunner(t)
	backend := graph.SchemaBackendNornicDB
	if strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")) == "neo4j" {
		backend = graph.SchemaBackendNeo4j
	}
	stmts, err := graph.SchemaStatementsForBackend(backend)
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

// advisoryGroupNodeProps reads back the group's scalar membership properties
// with plain MATCH+RETURN only, so the read itself stays portable across
// both backends.
func advisoryGroupNodeProps(t *testing.T, runner *boltRetractTestRunner, groupID string) (cves, advisories, members string) {
	t.Helper()
	rows, err := runner.runCypher(context.Background(),
		`MATCH (g:AdvisoryGroup {id: $id}) RETURN g.cve_ids AS cves, g.advisory_ids AS advisories, g.member_count AS members`,
		map[string]any{"id": groupID})
	if err != nil {
		t.Fatalf("read node %s: %v", groupID, err)
	}
	if len(rows) == 0 {
		return "", "", ""
	}
	return fmt.Sprint(rows[0]["cves"]), fmt.Sprint(rows[0]["advisories"]), fmt.Sprint(rows[0]["members"])
}

// advisoryGroupEdgePairs reads back (repository, package, ecosystem, purl)
// pairs with plain MATCH+RETURN only.
func advisoryGroupEdgePairs(t *testing.T, runner *boltRetractTestRunner, groupID string) string {
	t.Helper()
	rows, err := runner.runCypher(context.Background(),
		`MATCH (r:Repository)-[e:AFFECTED_BY]->(g:AdvisoryGroup {id: $id}) RETURN r.id AS repo, e.package_id AS pkg, e.ecosystem AS eco, e.purl AS purl ORDER BY repo, pkg`,
		map[string]any{"id": groupID})
	if err != nil {
		t.Fatalf("read edges %s: %v", groupID, err)
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf("%s|%s|%s|%s", row["repo"], row["pkg"], row["eco"], row["purl"]))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// TestBoltAdvisoryGroupReconcileLiveBackend is the #11161 regression: it
// drives the production AdvisoryGroupWriter against the live backend the
// harness points at — NornicDB for the golden-corpus gate default — and pins
// the full reconcile contract there (write, idempotent replay, retract,
// empty cleanup). Pre-fix it dead-letters on NornicDB with
// "reconcile advisory group ... UNWIND MATCH failed: end node variable ...";
// post-fix it is green on both NornicDB and pinned Neo4j.
func TestBoltAdvisoryGroupReconcileLiveBackend(t *testing.T) {
	runner, _ := openAdvisoryGroupLiveBackend(t)
	w := NewAdvisoryGroupWriter(countingBoltExecutor{runner: runner}, 0)
	ctx := context.Background()
	suffix := fmt.Sprintf("11161-%d", time.Now().UnixNano())
	repoA, repoB := "repository:"+suffix+"-a", "repository:"+suffix+"-b"
	groupID := "CVE-11161-" + suffix
	advisoryGroupLiveCleanup(t, runner, []string{groupID}, []string{repoA, repoB})
	advisoryGroupLiveSeedRepos(t, runner, repoA, repoB)

	group := advisorygroup.Group{
		ID:          groupID,
		CVEIDs:      []string{groupID},
		AdvisoryIDs: []string{"GHSA-11161-live"},
		Edges: []advisorygroup.Edge{
			{RepositoryID: repoA, PackageID: "pkg:npm/a", Ecosystem: "npm", PURL: "pkg:npm/a@1"},
			{RepositoryID: repoB, PackageID: "pkg:npm/a", Ecosystem: "npm", PURL: "pkg:npm/a@1"},
			{RepositoryID: repoB, PackageID: "pkg:npm/b", Ecosystem: "npm", PURL: "pkg:npm/b@2"},
			// Absent repository: must contribute nothing.
			{RepositoryID: "repository:" + suffix + "-absent", PackageID: "pkg:npm/a", Ecosystem: "npm", PURL: "pkg:npm/a@1"},
		},
	}
	first, err := w.ReconcileAdvisoryGroup(ctx, group)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Backend counters differ (NornicDB undercounts ON CREATE SET), so pin
	// the structure the contract needs: 3 edges created, nothing deleted.
	// Node creation itself is proven by the readback below.
	if first.EdgesCreated != 3 || first.EdgesDeleted != 0 || first.NodesDeleted != 0 {
		t.Fatalf("first reconcile stats = %+v, want 3 edges created and nothing deleted", first)
	}
	wantPairs := fmt.Sprintf("[%s|pkg:npm/a|npm|pkg:npm/a@1 %s|pkg:npm/a|npm|pkg:npm/a@1 %s|pkg:npm/b|npm|pkg:npm/b@2]", repoA, repoB, repoB)
	if got := advisoryGroupEdgePairs(t, runner, groupID); got != wantPairs {
		t.Fatalf("pairs = %s, want %s", got, wantPairs)
	}
	if cves, advisories, members := advisoryGroupNodeProps(t, runner, groupID); cves != "["+groupID+"]" || advisories != "[GHSA-11161-live]" || members != "2" {
		t.Fatalf("node = cves=%s advisories=%s members=%s, want cves=[%s] advisories=[GHSA-11161-live] members=2", cves, advisories, members, groupID)
	}

	// Replay: same desired state, zero write stats (no property churn).
	second, err := w.ReconcileAdvisoryGroup(ctx, group)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second != (advisorygroup.WriteStats{}) {
		t.Fatalf("replay stats = %+v, want all zero: a no-op reconcile must not churn properties", second)
	}
	if got := advisoryGroupEdgePairs(t, runner, groupID); got != wantPairs {
		t.Fatalf("after replay pairs = %s, want %s", got, wantPairs)
	}

	// Evolve the stamps: one more CVE (member_count 2->3) and a new purl on
	// one edge. The update path must resync both without creating anything.
	group.CVEIDs = append(group.CVEIDs, groupID+"-2")
	group.Edges[0].PURL = "pkg:npm/a@2"
	updated, err := w.ReconcileAdvisoryGroup(ctx, group)
	if err != nil {
		t.Fatalf("update reconcile: %v", err)
	}
	if updated.NodesCreated != 0 || updated.EdgesCreated != 0 || updated.NodesDeleted != 0 || updated.EdgesDeleted != 0 {
		t.Fatalf("update stats = %+v, want no creates or deletes", updated)
	}
	if updated.PropertiesSet == 0 {
		t.Fatalf("update stats = %+v, want resynced stamps to set properties", updated)
	}
	wantPairs = fmt.Sprintf("[%s|pkg:npm/a|npm|pkg:npm/a@2 %s|pkg:npm/a|npm|pkg:npm/a@1 %s|pkg:npm/b|npm|pkg:npm/b@2]", repoA, repoB, repoB)
	if got := advisoryGroupEdgePairs(t, runner, groupID); got != wantPairs {
		t.Fatalf("after update pairs = %s, want %s", got, wantPairs)
	}
	if cves, advisories, members := advisoryGroupNodeProps(t, runner, groupID); members != "3" || !strings.Contains(cves, groupID+"-2") {
		t.Fatalf("after update node = cves=%s advisories=%s members=%s, want resynced membership with count 3", cves, advisories, members)
	}

	// Drop one pair: only it retracts.
	group.Edges = group.Edges[:2]
	if _, err := w.ReconcileAdvisoryGroup(ctx, group); err != nil {
		t.Fatalf("retract reconcile: %v", err)
	}
	wantPairs = fmt.Sprintf("[%s|pkg:npm/a|npm|pkg:npm/a@2 %s|pkg:npm/a|npm|pkg:npm/a@1]", repoA, repoB)
	if got := advisoryGroupEdgePairs(t, runner, groupID); got != wantPairs {
		t.Fatalf("after retract pairs = %s, want %s", got, wantPairs)
	}

	// Empty reconcile deletes the node and its remaining edges.
	if _, err := w.ReconcileAdvisoryGroup(ctx, advisorygroup.Group{ID: groupID}); err != nil {
		t.Fatalf("empty reconcile: %v", err)
	}
	if got := advisoryGroupEdgePairs(t, runner, groupID); got != "[]" {
		t.Fatalf("after empty reconcile pairs = %q, want no edges", got)
	}
	if nodes, err := boltCount(ctx, runner, `MATCH (g:AdvisoryGroup {id: $id}) RETURN count(g) AS count`, map[string]any{"id": groupID}); err != nil || nodes != 0 {
		t.Fatalf("group nodes after empty reconcile = %d, %v; want 0", nodes, err)
	}
}

// TestBoltAdvisoryGroupParallelConvergenceLiveBackend proves the split
// create/update write path converges under the #10411 parallel contract on
// whatever backend the harness points at: disjoint groups reconciled in
// parallel and duplicate same-group reconciles racing on the group's one
// conflict key converge with no errors and no duplicated edges. The
// pre-#11161 single statement serialized duplicates on the group MERGE's
// exclusive lock; the edge merge keeps that MERGE for the same reason.
func TestBoltAdvisoryGroupParallelConvergenceLiveBackend(t *testing.T) {
	// Pinned to Neo4j: NornicDB does not serialize concurrent same-id
	// MERGEs, so duplicate same-group reconciles race the
	// advisory_group_id uniqueness constraint (commit-time UNIQUE
	// violation) instead of serializing on locks. That engine gap is
	// orthogonal to #11161 — the reconcile shape is proven
	// contention-free where the backend serializes MERGE, and the
	// NornicDB proof this issue needs is the single-threaded
	// idempotent/replay-zero contract above.
	if strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")) != "neo4j" {
		t.Skip("parallel convergence needs MERGE mutual exclusion; NornicDB races same-id MERGE (see #11161)")
	}
	runner, _ := openAdvisoryGroupLiveBackend(t)
	// Same production seam as the pinned suite: the persistent
	// RetryingExecutor absorbs transient deadlocks from concurrent
	// same-identity writers. Every statement the writer emits is
	// idempotent, which is what makes the retry safe.
	w := NewAdvisoryGroupWriter(
		&sourcecypher.RetryingExecutor{Inner: &boltTestExecutor{runner: runner}}, 0)
	ctx := context.Background()
	suffix := fmt.Sprintf("11161-par-%d", time.Now().UnixNano())
	repo := "repository:" + suffix
	advisoryGroupLiveSeedRepos(t, runner, repo)

	groups := make([]advisorygroup.Group, 0, 5)
	ids := make([]string, 0, 5)
	for i := range 5 {
		id := fmt.Sprintf("CVE-11161-PAR%d-%s", i, suffix)
		ids = append(ids, id)
		groups = append(groups, advisorygroup.Group{
			ID:          id,
			CVEIDs:      []string{id},
			AdvisoryIDs: []string{fmt.Sprintf("GHSA-11161-par-%d", i)},
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
		want := fmt.Sprintf("[%s|pkg:npm/par%d|npm|pkg:npm/par%d@1]", repo, i, i)
		if got := advisoryGroupEdgePairs(t, runner, id); got != want {
			t.Fatalf("group %s pairs = %q, want %q (no duplicates, no loss)", id, got, want)
		}
	}
}
