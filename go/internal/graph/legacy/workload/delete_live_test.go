// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package workload_test

import (
	"context"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
)

// TestLiveDeleteNeo4jIsTheGate proves DeleteNeo4jCypher on the pinned Neo4j:
// the statement itself re-evaluates every retirement clause, so a node that
// gained an instance, lost its twin's DEFINES, or gained an unsafe edge after
// a dry run is not deleted; the one eligible node is deleted with its twin and
// the twin's edges untouched; and a second run deletes nothing. NornicDB is
// excluded: it mis-evaluates the EXISTS clauses this statement relies on, and
// the route refuses execute there.
func TestLiveDeleteNeo4jIsTheGate(t *testing.T) {
	db := openLiveDB(t)
	f := seedFixture(t, db)
	ctx := context.Background()
	p1, twin := f.legacy("p1"), f.scoped("a", "p1")

	del := func(ids []string) (nodes, rels int) {
		t.Helper()
		counters, err := db.write(ctx, legacyworkload.DeleteNeo4jCypher, map[string]any{"ids": ids})
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		return counters.NodesDeleted(), counters.RelationshipsDeleted()
	}
	const exists = `MATCH (w:Workload {id: $id}) RETURN count(w) AS c`
	allIDs := f.legacyIDs()

	t.Run("gate: instance referenced by property after the dry run", func(t *testing.T) {
		db.mustWrite(t, `CREATE (:WorkloadInstance {id: $id, workload_id: $w})`,
			map[string]any{"id": "inst:late-" + f.nonce, "w": p1})
		if nodes, _ := del(allIDs); nodes != 0 {
			t.Fatalf("deleted %d nodes, want 0: p1 gained an instance", nodes)
		}
		db.mustWrite(t, `MATCH (i:WorkloadInstance {id: $id}) DETACH DELETE i`, map[string]any{"id": "inst:late-" + f.nonce})
	})
	t.Run("gate: unsafe relationship after the dry run", func(t *testing.T) {
		db.mustWrite(t, `MATCH (a:Repository {id: $a}), (b:Workload {id: $b}) CREATE (a)-[:DOCUMENTS]->(b)`,
			map[string]any{"a": f.repo("doc"), "b": p1})
		if nodes, _ := del(allIDs); nodes != 0 {
			t.Fatalf("deleted %d nodes, want 0: p1 gained a DOCUMENTS edge", nodes)
		}
		db.mustWrite(t, `MATCH (:Repository {id: $a})-[r:DOCUMENTS]->(:Workload {id: $b}) DELETE r`,
			map[string]any{"a": f.repo("doc"), "b": p1})
	})
	t.Run("gate: twin loses its DEFINES", func(t *testing.T) {
		db.mustWrite(t, `MATCH (:Repository {id: $a})-[r:DEFINES]->(:Workload {id: $b}) DELETE r`,
			map[string]any{"a": f.repo("a"), "b": twin})
		if nodes, _ := del(allIDs); nodes != 0 {
			t.Fatalf("deleted %d nodes, want 0: the twin is no longer defined by the repository", nodes)
		}
		db.mustWrite(t, `MATCH (r:Repository {id: $a}), (w:Workload {id: $b}) CREATE (r)-[:DEFINES]->(w)`,
			map[string]any{"a": f.repo("a"), "b": twin})
	})
	t.Run("empty id list deletes nothing and does not error", func(t *testing.T) {
		if nodes, _ := del([]string{}); nodes != 0 {
			t.Fatalf("deleted %d nodes for an empty list", nodes)
		}
	})

	twinEdges := func(label string) int64 {
		return db.count(t, `MATCH (:Workload {id: $id})-[r:`+label+`]-() RETURN count(r) AS c`, map[string]any{"id": twin})
	}
	wantDefines, wantSameName := twinEdges("DEFINES"), twinEdges("SAME_NAME")

	t.Run("deletes exactly the eligible node", func(t *testing.T) {
		nodes, rels := del(allIDs)
		if nodes != 1 {
			t.Fatalf("deleted %d nodes, want exactly 1 (p1)", nodes)
		}
		if rels != 2 {
			t.Fatalf("deleted %d relationships, want 2 (p1's DEFINES and SAME_NAME)", rels)
		}
		db.settledCount(t, exists, map[string]any{"id": p1}, 0, "p1 after delete")
		db.settledCount(t, exists, map[string]any{"id": twin}, 1, "twin after delete")
		for _, name := range caseNames[1:] {
			db.settledCount(t, exists, map[string]any{"id": f.legacy(name)}, 1, name+" must survive")
		}
		if got := twinEdges("DEFINES"); got != wantDefines {
			t.Errorf("twin DEFINES = %d, want %d unchanged", got, wantDefines)
		}
		if got := twinEdges("SAME_NAME"); got != wantSameName {
			t.Errorf("twin SAME_NAME = %d, want %d unchanged", got, wantSameName)
		}
	})
	t.Run("second run deletes nothing", func(t *testing.T) {
		if nodes, _ := del(allIDs); nodes != 0 {
			t.Fatalf("second run deleted %d nodes, want 0", nodes)
		}
	})
	t.Run("detection agrees after the delete", func(t *testing.T) {
		report, err := legacyworkload.Detect(ctx, db, legacyworkload.Options{IDs: allIDs})
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Verdicts) != len(caseNames)-1 || report.Eligible() != 0 {
			t.Fatalf("verdicts=%d eligible=%d, want %d blocked and none eligible", len(report.Verdicts), report.Eligible(), len(caseNames)-1)
		}
	})
}

// TestLiveDeleteNeo4jPlanSeeksTheIDConstraint pins the plan of the delete
// statement on the pinned Neo4j: the batch anchor and the twin lookup are
// unique-index seeks on the Workload id, and no operator scans the label.
func TestLiveDeleteNeo4jPlanSeeksTheIDConstraint(t *testing.T) {
	db := openLiveDB(t)
	ctx := context.Background()
	if _, err := db.write(ctx, `CREATE CONSTRAINT workload_id IF NOT EXISTS FOR (w:Workload) REQUIRE w.id IS UNIQUE`, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.write(ctx, `CREATE CONSTRAINT repository_id IF NOT EXISTS FOR (r:Repository) REQUIRE r.id IS UNIQUE`, nil); err != nil {
		t.Fatal(err)
	}
	session := db.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: db.database})
	defer func() { _ = session.Close(ctx) }()
	// EXPLAIN through the driver drops the outer branch of the semi-apply
	// operators, and a PROFILE of the DETACH DELETE would delete, so pin the
	// PROFILE plan of the same MATCH/WHERE with the delete swapped for a
	// read-only projection.
	readForm := strings.TrimSuffix(legacyworkload.DeleteNeo4jCypher, "DETACH DELETE w") + "RETURN w.id AS id"
	if readForm == legacyworkload.DeleteNeo4jCypher {
		t.Fatal("DeleteNeo4jCypher no longer ends with DETACH DELETE w; update the plan pin")
	}
	result, err := session.Run(ctx, "PROFILE "+readForm, map[string]any{"ids": []string{"workload:x"}})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	var walk func(p neo4jdriver.ProfiledPlan)
	walk = func(p neo4jdriver.ProfiledPlan) {
		ops = append(ops, p.Operator())
		for _, c := range p.Children() {
			walk(c)
		}
	}
	walk(summary.Profile())
	joined := strings.Join(ops, " ")
	if strings.Count(joined, "NodeUniqueIndexSeek") < 2 {
		t.Errorf("plan = %s: want NodeUniqueIndexSeek for both the id batch anchor and the twin lookup", joined)
	}
	for _, bad := range []string{"AllNodesScan", "NodeByLabelScan", "NodeIndexScan"} {
		if strings.Contains(joined, bad) {
			t.Errorf("plan = %s: contains %s, the delete must anchor on id seeks", joined, bad)
		}
	}
}
