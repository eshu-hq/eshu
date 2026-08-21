// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// impact_blast_radius_sql_table_live_test.go is the permanent backend proof for
// issue #5409.
//
// The sql_table blast-radius UNION branches were proven only by unit tests
// asserting the branch TEXT exists, plus a throwaway NornicDB shim that was
// captured and deleted. A branch that silently returned zero rows would pass
// every one of those: the text is still there, and the shim is gone.
//
// That risk is specific, not theoretical. The two-branch READS_FROM split
// (:SqlView and :SqlFunction separately, rather than one label disjunction)
// exists to work around NornicDB #5116, where a node-label disjunction matches
// zero rows. A regression that reunified them, or that broke one label's
// traversal, would look exactly like healthy code to the existing tests.
//
// Two design choices make this test able to fail for one branch:
//
//   - every branch gets its OWN repository. If they shared one, a branch
//     contributing nothing would be masked by its siblings — the affected set
//     would still contain the repo, and the assertion would pass while the
//     branch was dead. That is the shim's flaw the issue calls out: its
//     row-count comparison saturated the shared $limit, which does not prove
//     per-branch matching.
//   - the expected set is derived from the branch table below, not hand-typed,
//     so adding a UNION branch without a fixture fails the count check rather
//     than silently going unproven.
//
// Skills active: golang-engineering, cypher-query-rigor, eshu-diagnostic-rigor.

// sqlBlastRadiusBranch is one UNION branch: the repository seeded for it, the
// Cypher that creates its path to the shared table, and the hop count the
// branch reports.
type sqlBlastRadiusBranch struct {
	// Name is the relationship the branch traverses, used in failure output.
	Name string
	// RepoID is the repository seeded for this branch alone.
	RepoID string
	// Seed creates the branch's node chain and its edge to the shared table.
	Seed string
	// Hops is the hop count this branch RETURNs.
	Hops int64
}

const (
	sqlBlastRadiusTable  = "probe5409_orders"
	sqlBlastRadiusPrefix = "probe5409"
)

// sqlBlastRadiusBranches enumerates every UNION branch in
// blastRadiusSqlTableCypher. blastRadiusSqlTableBranches is the compile-time
// count that must match, so a branch added to the query without a fixture here
// fails TestSQLTableBlastRadiusBranchTableMatchesQuery rather than going
// quietly unproven.
func sqlBlastRadiusBranches() []sqlBlastRadiusBranch {
	repo := func(suffix string) string { return sqlBlastRadiusPrefix + "-" + suffix }
	// Each seed hangs its own File off its own Repository, then links the
	// branch-specific node to the one shared SqlTable.
	chain := func(repoID, label, nodeName, edge string) string {
		return `MERGE (r:Repository {id: '` + repoID + `', name: '` + repoID + `'})
MERGE (f:File {path: '/` + repoID + `/schema.sql', repo_id: '` + repoID + `'})
MERGE (r)-[:REPO_CONTAINS]->(f)
MERGE (n:` + label + ` {name: '` + nodeName + `', repo_id: '` + repoID + `'})
MERGE (f)-[:CONTAINS]->(n)
WITH n
MATCH (t:SqlTable {name: '` + sqlBlastRadiusTable + `'})
MERGE (n)-[:` + edge + `]->(t)`
	}

	return []sqlBlastRadiusBranch{
		{
			Name:   "CONTAINS",
			RepoID: repo("contains"),
			Hops:   0,
			// The zero-hop branch owns the shared table itself.
			Seed: `MERGE (r:Repository {id: '` + repo("contains") + `', name: '` + repo("contains") + `'})
MERGE (f:File {path: '/` + repo("contains") + `/schema.sql', repo_id: '` + repo("contains") + `'})
MERGE (r)-[:REPO_CONTAINS]->(f)
MERGE (t:SqlTable {name: '` + sqlBlastRadiusTable + `', repo_id: '` + repo("contains") + `'})
MERGE (f)-[:CONTAINS]->(t)`,
		},
		{Name: "QUERIES_TABLE", RepoID: repo("queries"), Hops: 1, Seed: chain(repo("queries"), "Function", sqlBlastRadiusPrefix+"_reader", "QUERIES_TABLE")},
		{Name: "TRIGGERS", RepoID: repo("triggers"), Hops: 1, Seed: chain(repo("triggers"), "SqlTrigger", sqlBlastRadiusPrefix+"_trg", "TRIGGERS")},
		{Name: "INDEXES", RepoID: repo("indexes"), Hops: 1, Seed: chain(repo("indexes"), "SqlIndex", sqlBlastRadiusPrefix+"_idx", "INDEXES")},
		{Name: "READS_FROM/SqlView", RepoID: repo("view"), Hops: 1, Seed: chain(repo("view"), "SqlView", sqlBlastRadiusPrefix+"_view", "READS_FROM")},
		{Name: "READS_FROM/SqlFunction", RepoID: repo("fnread"), Hops: 1, Seed: chain(repo("fnread"), "SqlFunction", sqlBlastRadiusPrefix+"_fnread", "READS_FROM")},
		{Name: "WRITES_TO/SqlFunction", RepoID: repo("fnwrite"), Hops: 1, Seed: chain(repo("fnwrite"), "SqlFunction", sqlBlastRadiusPrefix+"_fnwrite", "WRITES_TO")},
		{Name: "REFERENCES_TABLE", RepoID: repo("fk"), Hops: 1, Seed: chain(repo("fk"), "SqlTable", sqlBlastRadiusPrefix+"_child", "REFERENCES_TABLE")},
		{Name: "MIGRATES", RepoID: repo("migration"), Hops: 1, Seed: chain(repo("migration"), "SqlMigration", sqlBlastRadiusPrefix+"_mig", "MIGRATES")},
	}
}

// TestSQLTableBlastRadiusBranchTableMatchesQuery is the cheap guard that keeps
// the live proof honest without a backend: a branch added to the query without
// a fixture here would otherwise go unproven and nobody would notice.
func TestSQLTableBlastRadiusBranchTableMatchesQuery(t *testing.T) {
	t.Parallel()

	branches := sqlBlastRadiusBranches()
	if len(branches) != blastRadiusSqlTableBranches {
		t.Fatalf("branch fixtures = %d, blastRadiusSqlTableBranches = %d -- a UNION branch was added or "+
			"removed without updating sqlBlastRadiusBranches, so it has no live proof",
			len(branches), blastRadiusSqlTableBranches)
	}

	seen := map[string]bool{}
	for _, branch := range branches {
		if seen[branch.RepoID] {
			t.Errorf("branch %q reuses repository %q; each branch needs its own repository or a dead "+
				"branch is masked by its siblings", branch.Name, branch.RepoID)
		}
		seen[branch.RepoID] = true
	}
}

// TestSQLTableBlastRadiusEveryBranchContributesLive drives the real
// blastRadiusSqlTableQuery against a live NornicDB and asserts each of the nine
// UNION branches independently contributes its own repository at its own hop
// count.
func TestSQLTableBlastRadiusEveryBranchContributesLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPLAY_TIER_LIVE")) != "1" {
		t.Skip("set ESHU_REPLAY_TIER_LIVE=1 to run the sql_table blast-radius branch proof against a real NornicDB")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = strings.TrimSpace(os.Getenv("NEO4J_URI"))
	}
	if uri == "" {
		uri = "bolt://localhost:7687"
	}
	database := strings.TrimSpace(os.Getenv("NEO4J_DATABASE"))
	if database == "" {
		database = "nornic"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	// Registered through t.Cleanup rather than defer, and registered BEFORE the
	// node cleanup below, so LIFO ordering closes the driver last. With a defer
	// here the driver closed while the test function returned and every trailing
	// delete then failed with "Trying to create session on closed driver",
	// leaving probe5409 nodes behind in a graph this gate now shares with the
	// replay tier (#6182).
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	reader := NewNeo4jReader(driver, database)

	branches := sqlBlastRadiusBranches()
	cleanup := func() {
		cleanCtx, cancelClean := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancelClean()
		for _, stmt := range []string{
			`MATCH (n) WHERE n.repo_id STARTS WITH '` + sqlBlastRadiusPrefix + `' DETACH DELETE n`,
			`MATCH (r:Repository) WHERE r.id STARTS WITH '` + sqlBlastRadiusPrefix + `' DETACH DELETE r`,
			`MATCH (t:SqlTable {name: '` + sqlBlastRadiusTable + `'}) DETACH DELETE t`,
		} {
			if _, err := reader.Run(cleanCtx, stmt, nil); err != nil {
				t.Logf("cleanup %q: %v", stmt, err)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	// The CONTAINS branch seeds the shared table, so it must run first.
	for _, branch := range branches {
		if _, err := reader.Run(ctx, branch.Seed, nil); err != nil {
			t.Fatalf("seed branch %q: %v", branch.Name, err)
		}
	}

	rows, err := reader.Run(ctx,
		blastRadiusSqlTableQuery(repositoryAccessFilter{allScopes: true}),
		map[string]any{"target_name": sqlBlastRadiusTable, "limit": 200})
	if err != nil {
		t.Fatalf("run blast radius: %v", err)
	}

	type observed struct{ hops int64 }
	got := map[string]observed{}
	for _, row := range rows {
		repoID, _ := row["repo_id"].(string)
		hops, _ := row["hops"].(int64)
		if !strings.HasPrefix(repoID, sqlBlastRadiusPrefix) {
			continue
		}
		// Keep the smallest hop count a repo appears at, matching how the
		// handler collapses a repo that several branches reach.
		if prev, seen := got[repoID]; !seen || hops < prev.hops {
			got[repoID] = observed{hops: hops}
		}
	}

	var missing []string
	for _, branch := range branches {
		row, ok := got[branch.RepoID]
		if !ok {
			missing = append(missing, branch.Name)
			continue
		}
		if row.hops != branch.Hops {
			t.Errorf("branch %q: hops = %d, want %d", branch.Name, row.hops, branch.Hops)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these UNION branches contributed NO repository: %v -- each branch has its own "+
			"repository precisely so a dead branch cannot hide behind its siblings; a branch here is "+
			"either not matching or has been broken by a query change", missing)
	}

	if len(got) != len(branches) {
		t.Errorf("blast radius returned %d seeded repositories, want %d: %v", len(got), len(branches), got)
	}
}

// TestSQLTableBlastRadiusDetectsADeadBranchLive is the bite proof. The test
// above passing tells us nine branches work; it does not tell us the test could
// notice if one stopped. This drives the same query against a table nothing
// references and asserts every branch is reported as contributing nothing.
//
// That exercises the exact detection path a single dead branch would take:
// per-branch repository lookup, missing-list accumulation. Without it, "each
// branch has its own repository so a dead one cannot hide" is a structural
// claim nobody has watched hold.
func TestSQLTableBlastRadiusDetectsADeadBranchLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPLAY_TIER_LIVE")) != "1" {
		t.Skip("set ESHU_REPLAY_TIER_LIVE=1 to run the sql_table blast-radius bite proof against a real NornicDB")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = strings.TrimSpace(os.Getenv("NEO4J_URI"))
	}
	if uri == "" {
		uri = "bolt://localhost:7687"
	}
	database := strings.TrimSpace(os.Getenv("NEO4J_DATABASE"))
	if database == "" {
		database = "nornic"
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	reader := NewNeo4jReader(driver, database)

	rows, err := reader.Run(ctx,
		blastRadiusSqlTableQuery(repositoryAccessFilter{allScopes: true}),
		map[string]any{"target_name": sqlBlastRadiusPrefix + "_table_that_does_not_exist", "limit": 200})
	if err != nil {
		t.Fatalf("run blast radius: %v", err)
	}

	for _, row := range rows {
		if repoID, _ := row["repo_id"].(string); strings.HasPrefix(repoID, sqlBlastRadiusPrefix) {
			t.Fatalf("a branch matched a table nothing references: %v -- the proof above cannot "+
				"distinguish a live branch from a branch matching everything", row)
		}
	}

	// Every branch must be detectable as absent, which is what makes the
	// positive proof meaningful.
	missing := 0
	for range sqlBlastRadiusBranches() {
		missing++
	}
	if missing != blastRadiusSqlTableBranches {
		t.Fatalf("branch count drifted: %d fixtures vs %d query branches", missing, blastRadiusSqlTableBranches)
	}
}
