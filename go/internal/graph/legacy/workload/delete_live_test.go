// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package workload_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
)

// TestLiveDeleteNeo4jIsTheGate proves DeleteNeo4jCypher on the pinned Neo4j:
// the statement itself re-evaluates every retirement clause, so a node that
// gained an instance, lost its twin's DEFINES, or gained an unsafe edge after
// a dry run is not deleted; the one eligible node is deleted with its twin and
// the twin's edges untouched; and a second run deletes nothing. Only
// Neo4j is tested, by owner direction; the route refuses execute elsewhere.
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
	allIDs := append(f.legacyIDs(), f.extraIDs()...)

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
		for _, id := range f.extraIDs() {
			db.settledCount(t, exists, map[string]any{"id": id}, 1, id+" must survive")
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
		report, err := legacyworkload.Detect(ctx, db, legacyworkload.Options{IDs: f.legacyIDs()})
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
// unique-index seeks on the Workload id, and no operator scans a label. It
// applies the production schema statements the plan depends on itself, so it
// holds on a fresh database with no schema bootstrap (the live CI job).
func TestLiveDeleteNeo4jPlanSeeksTheIDConstraint(t *testing.T) {
	db := openLiveDB(t)
	ctx := context.Background()
	applyProductionSchema(t, db, "workload_id", "repository_id", "workload_instance_workload_id")
	session := db.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: db.database})
	defer func() { _ = session.Close(ctx) }()
	// PROFILE the real statement (EXPLAIN through the driver drops the outer
	// branch of the semi-apply operators). The id names no node, so the
	// profile deletes and sets nothing.
	result, err := session.Run(ctx, "PROFILE "+legacyworkload.DeleteNeo4jCypher, map[string]any{"ids": []string{"workload:x-plan-pin-absent"}})
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

// TestLiveDetectionAndDeleteStatementSelectTheSameIDs proves the Go detection
// path and the in-statement Neo4j predicate cannot disagree: on the nine-case
// fixture, and after each late change a dry run could miss, the ids Detect
// marks eligible equal the ids the delete statement's own MATCH/WHERE selects
// (the statement run as a read). Readiness, the dry run, and the delete gate
// therefore answer alike.
func TestLiveDetectionAndDeleteStatementSelectTheSameIDs(t *testing.T) {
	db := openLiveDB(t)
	f := seedFixture(t, db)
	ctx := context.Background()
	seedPadded(t, db, f)
	allIDs := append(append(f.legacyIDs(), f.extraIDs()...), f.paddedIDs()...)
	p1, twin := f.legacy("p1"), f.scoped("a", "p1")

	readForm := gateReadForm(t)
	assertSame := func(label string) {
		t.Helper()
		report, err := legacyworkload.Detect(ctx, db, legacyworkload.Options{IDs: allIDs})
		if err != nil {
			t.Fatal(err)
		}
		var goIDs []string
		for _, v := range report.Verdicts {
			if v.Eligible {
				goIDs = append(goIDs, v.ID)
			}
		}
		rows, err := db.Run(ctx, readForm, map[string]any{"ids": allIDs})
		if err != nil {
			t.Fatalf("%s: statement predicate read: %v", label, err)
		}
		var stmtIDs []string
		for _, row := range rows {
			stmtIDs = append(stmtIDs, row["id"].(string))
		}
		slices.Sort(goIDs)
		slices.Sort(stmtIDs)
		if !slices.Equal(goIDs, stmtIDs) {
			t.Fatalf("%s: Detect selects %v but the delete statement selects %v", label, goIDs, stmtIDs)
		}
	}

	assertSame("base fixture")
	rows, _ := db.Run(ctx, readForm, map[string]any{"ids": allIDs})
	if len(rows) != 1 || rows[0]["id"] != p1 {
		t.Fatalf("base fixture: statement selects %v, want exactly p1", rows)
	}
	// A padded name or repository id has a trimmed twin the delete cannot
	// match, so neither side selects it; Detect reports it no_twin.
	padded, err := legacyworkload.Detect(ctx, db, legacyworkload.Options{IDs: f.paddedIDs()})
	if err != nil {
		t.Fatal(err)
	}
	if len(padded.Verdicts) != len(f.paddedIDs()) {
		t.Fatalf("padded leftovers: got %d verdicts, want %d", len(padded.Verdicts), len(f.paddedIDs()))
	}
	for _, v := range padded.Verdicts {
		if v.Eligible || v.BlockedReason != legacyworkload.BlockedNoTwin {
			t.Fatalf("padded leftover %q: eligible=%v reason=%q, want no_twin", v.ID, v.Eligible, v.BlockedReason)
		}
	}
	if padded.WithScopedTwin() != 0 {
		t.Fatalf("padded leftovers: WithScopedTwin = %d, want 0 (readiness does not count them)", padded.WithScopedTwin())
	}

	db.mustWrite(t, `CREATE (:WorkloadInstance {id: $id, workload_id: $w})`, map[string]any{"id": "inst:late-" + f.nonce, "w": p1})
	assertSame("late instance by property")
	db.mustWrite(t, `MATCH (i:WorkloadInstance {id: $id}) DETACH DELETE i`, map[string]any{"id": "inst:late-" + f.nonce})

	db.mustWrite(t, `MATCH (a:Repository {id: $a}), (b:Workload {id: $b}) CREATE (a)-[:DOCUMENTS]->(b)`, map[string]any{"a": f.repo("doc"), "b": p1})
	assertSame("late DOCUMENTS edge")
	db.mustWrite(t, `MATCH (:Repository {id: $a})-[r:DOCUMENTS]->(:Workload {id: $b}) DELETE r`, map[string]any{"a": f.repo("doc"), "b": p1})

	db.mustWrite(t, `MATCH (:Repository {id: $a})-[r:DEFINES]->(:Workload {id: $b}) DELETE r`, map[string]any{"a": f.repo("a"), "b": twin})
	assertSame("twin loses its DEFINES")
	db.mustWrite(t, `MATCH (r:Repository {id: $a}), (w:Workload {id: $b}) CREATE (r)-[:DEFINES]->(w)`, map[string]any{"a": f.repo("a"), "b": twin})

	db.mustWrite(t, `MATCH (a:Workload {id: $a}), (b:Workload {id: $b}) CREATE (a)-[:SAME_NAME]->(b)`, map[string]any{"a": p1, "b": f.scoped("e", "n4")})
	assertSame("extra SAME_NAME to another workload stays eligible")
	db.mustWrite(t, `MATCH (a:Repository {id: $a}), (b:Workload {id: $b}) CREATE (a)-[:SAME_NAME]->(b)`, map[string]any{"a": f.repo("doc"), "b": p1})
	assertSame("SAME_NAME from a non-Workload blocks")
	rows, _ = db.Run(ctx, readForm, map[string]any{"ids": allIDs})
	if len(rows) != 0 {
		t.Fatalf("SAME_NAME from a Repository must block, statement selects %v", rows)
	}
}

// gateReadForm is DeleteNeo4jCypher with the lock markers and the delete
// removed: the same MATCH and retirement clauses, run as a read.
func gateReadForm(t *testing.T) string {
	t.Helper()
	const lock = "SET w.__eshu_retire_lock = true\nREMOVE w.__eshu_retire_lock\n"
	stmt := legacyworkload.DeleteNeo4jCypher
	if !strings.Contains(stmt, lock) || !strings.HasSuffix(stmt, "DETACH DELETE w") {
		t.Fatal("DeleteNeo4jCypher no longer has the lock markers or no longer ends with DETACH DELETE w; update gateReadForm")
	}
	return strings.TrimSuffix(strings.Replace(stmt, lock, "", 1), "DETACH DELETE w") + "RETURN w.id AS id"
}

// applyProductionSchema applies the named production Neo4j schema statements
// (from graph.SchemaStatementsForBackend, so the test cannot diverge from the
// production DDL) and waits for the indexes to come online.
func applyProductionSchema(t *testing.T, db *liveDB, names ...string) {
	t.Helper()
	statements, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		found := false
		for _, stmt := range statements {
			if strings.Contains(stmt, " "+name+" IF NOT EXISTS ") {
				db.mustWrite(t, stmt, nil)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("production schema has no statement named %s", name)
		}
	}
	db.mustWrite(t, `CALL db.awaitIndexes(120)`, nil)
}

// TestLiveDeleteNeo4jRechecksAfterTheLockWait is the concurrent-writer proof
// for DeleteNeo4jCypher. A writer transaction (T1) changes the eligible node
// p1 and stays open while the delete runs; T1 then commits. The delete takes
// p1's lock before it evaluates any clause, so:
//
//   - a property write on p1 holds p1's lock; the delete waits, then reads the
//     committed value and keeps p1 (nothing deleted, no error);
//   - an INSTANCE_OF edge created into p1 makes the two transactions wait on
//     each other; Neo4j aborts one. Whichever it is, the graph never holds a
//     WorkloadInstance whose INSTANCE_OF edge the delete removed, and a retry
//     of the delete keeps p1.
func TestLiveDeleteNeo4jRechecksAfterTheLockWait(t *testing.T) {
	db := openLiveDB(t)
	ctx := context.Background()
	exists := `MATCH (w:Workload {id: $id}) RETURN count(w) AS c`

	race := func(t *testing.T, f fixture, writer string, params map[string]any) (deleted int, delErr, commitErr error) {
		t.Helper()
		session := db.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: db.database})
		defer func() { _ = session.Close(ctx) }()
		tx, err := session.BeginTransaction(ctx)
		if err != nil {
			t.Fatal(err)
		}
		res, err := tx.Run(ctx, writer, params)
		if err == nil {
			_, err = res.Consume(ctx)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("T1 write: %v", err)
		}
		type outcome struct {
			nodes int
			err   error
		}
		done := make(chan outcome, 1)
		go func() {
			counters, err := db.write(ctx, legacyworkload.DeleteNeo4jCypher, map[string]any{"ids": []string{f.legacy("p1")}})
			if err != nil {
				done <- outcome{err: err}
				return
			}
			done <- outcome{nodes: counters.NodesDeleted()}
		}()
		select {
		case o := <-done:
			_ = tx.Rollback(ctx)
			t.Fatalf("delete finished while T1 was open (nodes=%d err=%v); it must wait on T1", o.nodes, o.err)
		case <-time.After(time.Second):
		}
		commitErr = tx.Commit(ctx)
		o := <-done
		return o.nodes, o.err, commitErr
	}

	t.Run("property write on the node is re-read after the lock", func(t *testing.T) {
		f := seedFixture(t, db)
		p1 := f.legacy("p1")
		nodes, delErr, commitErr := race(t, f, `MATCH (w:Workload {id: $w}) SET w.generation_id = 'g-late'`, map[string]any{"w": p1})
		if commitErr != nil || delErr != nil {
			t.Fatalf("commit err=%v delete err=%v, want both nil: the delete must wait, not deadlock", commitErr, delErr)
		}
		if nodes != 0 {
			t.Fatalf("deleted %d nodes, want 0: p1 gained a generation_id while the delete waited", nodes)
		}
		db.settledCount(t, exists, map[string]any{"id": p1}, 1, "p1 after the race")
	})

	for round := range 3 {
		t.Run(fmt.Sprintf("instance edge created into the node, round %d", round), func(t *testing.T) {
			f := seedFixture(t, db)
			p1, inst := f.legacy("p1"), "inst:race-"+f.nonce
			_, delErr, commitErr := race(t, f,
				`MATCH (w:Workload {id: $w}) CREATE (:WorkloadInstance {id: $i, workload_id: 'none'})-[:INSTANCE_OF]->(w)`,
				map[string]any{"w": p1, "i": inst})
			for _, err := range []error{delErr, commitErr} {
				if err != nil && !strings.Contains(err.Error(), "DeadlockDetected") {
					t.Fatalf("unexpected error %v: only a deadlock abort is expected", err)
				}
			}
			p1Left := db.count(t, exists, map[string]any{"id": p1})
			instances := db.count(t, `MATCH (i:WorkloadInstance {id: $i}) RETURN count(i) AS c`, map[string]any{"i": inst})
			attached := db.count(t, `MATCH (:WorkloadInstance {id: $i})-[:INSTANCE_OF]->(:Workload {id: $w}) RETURN count(*) AS c`, map[string]any{"i": inst, "w": p1})
			switch {
			case commitErr == nil && (p1Left != 1 || attached != 1):
				t.Fatalf("T1 committed but p1=%d attached=%d: the delete removed a node that gained an instance", p1Left, attached)
			case commitErr != nil && instances != 0:
				t.Fatalf("T1 aborted but its instance exists (%d)", instances)
			}
			// The retry the executor performs after a transient abort must keep p1.
			if commitErr == nil {
				counters, err := db.write(ctx, legacyworkload.DeleteNeo4jCypher, map[string]any{"ids": []string{p1}})
				if err != nil || counters.NodesDeleted() != 0 {
					t.Fatalf("retry deleted %v (err %v), want 0", counters, err)
				}
			}
			t.Logf("round %d: delete err=%v, T1 commit err=%v, p1=%d attached=%d", round, delErr != nil, commitErr != nil, p1Left, attached)
		})
	}
}
