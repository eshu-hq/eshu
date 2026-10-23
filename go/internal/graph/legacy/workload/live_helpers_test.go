// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package workload_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// liveDB is a minimal Bolt client for the seeded proofs. It is gated on
// ESHU_CYPHER_BOLT_DSN (or ESHU_NEO4J_URI) and skips otherwise.
type liveDB struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func openLiveDB(t *testing.T) *liveDB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	}
	if dsn == "" {
		t.Skip("ESHU_CYPHER_BOLT_DSN/ESHU_NEO4J_URI not set; skipping #8795 live legacy workload proof")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))
	if database == "" {
		database = strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	}
	if database == "" {
		database = "neo4j"
	}
	driver, err := neo4jdriver.NewDriverWithContext(dsn, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open bolt driver: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("verify bolt connectivity: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	return &liveDB{driver: driver, database: database}
}

// Run satisfies legacyworkload.Reader.
func (db *liveDB) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := db.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: db.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		rows = append(rows, rec.AsMap())
	}
	return rows, nil
}

// write runs one auto-commit write statement and returns its write counters.
func (db *liveDB) write(ctx context.Context, cypher string, params map[string]any) (neo4jdriver.Counters, error) {
	session := db.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: db.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return nil, err
	}
	return summary.Counters(), nil
}

func (db *liveDB) mustWrite(t *testing.T, cypher string, params map[string]any) {
	t.Helper()
	if _, err := db.write(context.Background(), cypher, params); err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
}

// count runs a statement that returns one integer column named c.
func (db *liveDB) count(t *testing.T, cypher string, params map[string]any) int64 {
	t.Helper()
	rows, err := db.Run(context.Background(), cypher, params)
	if err != nil || len(rows) != 1 {
		t.Fatalf("count %q: rows=%v err=%v", cypher, rows, err)
	}
	n, _ := rows[0]["c"].(int64)
	return n
}

// settledCount re-reads until the count equals want or 30 seconds pass, so a
// stale post-write read cannot pass or fail a case.
func (db *liveDB) settledCount(t *testing.T, cypher string, params map[string]any, want int64, what string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		got := db.count(t, cypher, params)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: count = %d, want %d after 30s of settling", what, got, want)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// fixture is one nonce-scoped seeded graph covering every retirement case.
type fixture struct {
	nonce string
	// ids by case name; each is the legacy node id except for scoped cases.
	id map[string]string
}

// caseNames lists the legacy-shaped cases in a stable order.
var caseNames = []string{"p1", "n2", "n2b", "n3", "n4", "n5", "n5b", "n6"}

func (f fixture) repo(name string) string { return "repository:" + f.nonce + ":" + name }
func (f fixture) legacy(name string) string {
	return "workload:" + name + "-" + f.nonce
}

func (f fixture) scoped(repo, name string) string {
	return "workload:" + f.repo(repo) + ":" + name + "-" + f.nonce
}

// seed writes the fixture with one statement per node or edge: NornicDB
// rejects a multi-clause CREATE chain.
func seedFixture(t *testing.T, db *liveDB) fixture {
	t.Helper()
	nonce := fmt.Sprintf("p8795-%d", time.Now().UnixNano())
	f := fixture{nonce: nonce, id: map[string]string{}}
	t.Cleanup(func() {
		_, err := db.write(context.Background(),
			`MATCH (n) WHERE n.id CONTAINS $nonce OR n.workload_id CONTAINS $nonce DETACH DELETE n`,
			map[string]any{"nonce": nonce})
		if err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	repos := []string{"a", "b", "c", "c2", "d", "e", "f", "g", "h", "x", "doc"}
	for _, r := range repos {
		db.mustWrite(t, `CREATE (:Repository {id: $id})`, map[string]any{"id": f.repo(r)})
	}
	workload := func(id, name, repo string, scoped bool) {
		params := map[string]any{"id": id, "name": name + "-" + nonce, "repo": f.repo(repo)}
		if scoped {
			db.mustWrite(t, `CREATE (:Workload {id: $id, name: $name, repo_id: $repo, generation_id: 'g1'})`, params)
		} else {
			db.mustWrite(t, `CREATE (:Workload {id: $id, name: $name, repo_id: $repo})`, params)
		}
	}
	defines := func(repo, id string) {
		db.mustWrite(t, `MATCH (r:Repository {id: $r}), (w:Workload {id: $w}) CREATE (r)-[:DEFINES]->(w)`,
			map[string]any{"r": f.repo(repo), "w": id})
	}
	edge := func(cypher, a, b string) {
		db.mustWrite(t, cypher, map[string]any{"a": a, "b": b})
	}
	const wwEdge = `MATCH (a:Workload {id: $a}), (b:Workload {id: $b}) CREATE (a)-[:%s]->(b)`

	// P1: leftover with a same-repo twin, only DEFINES-from-own-repo and SAME_NAME.
	workload(f.scoped("a", "p1"), "p1", "a", true)
	workload(f.legacy("p1"), "p1", "a", false)
	defines("a", f.scoped("a", "p1"))
	defines("a", f.legacy("p1"))
	workload(f.scoped("x", "p1"), "p1", "x", true)
	defines("x", f.scoped("x", "p1"))
	edge(fmt.Sprintf(wwEdge, "SAME_NAME"), f.legacy("p1"), f.scoped("x", "p1"))
	edge(fmt.Sprintf(wwEdge, "SAME_NAME"), f.scoped("a", "p1"), f.scoped("x", "p1"))

	// N1: a scoped node with a null generation_id is never a candidate.
	db.mustWrite(t, `CREATE (:Workload {id: $id, name: $name, repo_id: $repo})`,
		map[string]any{"id": f.scoped("b", "n1"), "name": "n1-" + nonce, "repo": f.repo("b")})
	defines("b", f.scoped("b", "n1"))

	// N2: instance by INSTANCE_OF edge.
	workload(f.scoped("c", "n2"), "n2", "c", true)
	workload(f.legacy("n2"), "n2", "c", false)
	defines("c", f.scoped("c", "n2"))
	defines("c", f.legacy("n2"))
	db.mustWrite(t, `CREATE (:WorkloadInstance {id: $id, workload_id: $w})`,
		map[string]any{"id": "inst:n2-" + nonce, "w": f.legacy("n2")})
	edge(`MATCH (a:WorkloadInstance {id: $a}), (b:Workload {id: $b}) CREATE (a)-[:INSTANCE_OF]->(b)`, "inst:n2-"+nonce, f.legacy("n2"))

	// N2b: instance by the workload_id property only.
	workload(f.scoped("c2", "n2b"), "n2b", "c2", true)
	workload(f.legacy("n2b"), "n2b", "c2", false)
	defines("c2", f.scoped("c2", "n2b"))
	defines("c2", f.legacy("n2b"))
	db.mustWrite(t, `CREATE (:WorkloadInstance {id: $id, workload_id: $w})`,
		map[string]any{"id": "inst:n2b-" + nonce, "w": f.legacy("n2b")})

	// N3: no twin at all.
	workload(f.legacy("n3"), "n3", "d", false)
	defines("d", f.legacy("n3"))

	// N4: the same-named twin lives in a different repository only.
	workload(f.legacy("n4"), "n4", "d", false)
	defines("d", f.legacy("n4"))
	workload(f.scoped("e", "n4"), "n4", "e", true)
	defines("e", f.scoped("e", "n4"))

	// N5: twin present but a DOCUMENTS edge blocks.
	workload(f.scoped("f", "n5"), "n5", "f", true)
	workload(f.legacy("n5"), "n5", "f", false)
	defines("f", f.scoped("f", "n5"))
	defines("f", f.legacy("n5"))
	edge(`MATCH (a:Repository {id: $a}), (b:Workload {id: $b}) CREATE (a)-[:DOCUMENTS]->(b)`, f.repo("doc"), f.legacy("n5"))

	// N5b: twin present but a DEPENDS_ON edge blocks.
	workload(f.scoped("g", "n5b"), "n5b", "g", true)
	workload(f.legacy("n5b"), "n5b", "g", false)
	defines("g", f.scoped("g", "n5b"))
	defines("g", f.legacy("n5b"))
	edge(fmt.Sprintf(wwEdge, "DEPENDS_ON"), f.legacy("n5b"), f.scoped("g", "n5b"))

	// N6: a scoped twin exists but its repository does not DEFINE it.
	workload(f.scoped("h", "n6"), "n6", "h", true)
	workload(f.legacy("n6"), "n6", "h", false)
	defines("h", f.legacy("n6"))

	for _, name := range caseNames {
		f.id[name] = f.legacy(name)
	}
	return f
}

func (f fixture) legacyIDs() []string {
	ids := make([]string, 0, len(caseNames))
	for _, name := range caseNames {
		ids = append(ids, f.legacy(name))
	}
	return ids
}
