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
// The detection itself -- sqlBlastRadiusMissingBranches, which accumulates the
// missing list from the rows the query returned -- is guarded by
// TestSQLTableBlastRadiusReportsUnseededBranchMissingLive (#6204). Both tests
// call that one helper, so the bite proof guards the code this test runs rather
// than a copy of it.
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
	// sqlBlastRadiusPrefix namespaces every repository and node the nine-branch
	// proof seeds, so its cleanup cannot reach anything else in the graph this
	// gate shares with the replay tier.
	sqlBlastRadiusPrefix = "probe5409"
	// sqlBlastRadiusBitePrefix namespaces the bite proof's fixtures (#6204). It
	// deliberately does NOT share sqlBlastRadiusPrefix: both tests run against
	// the same graph inside one gate invocation, and a shared prefix would let
	// one test's rows answer the other's question.
	sqlBlastRadiusBitePrefix = "probe6204"
)

// sqlBlastRadiusTableFor names the one shared SqlTable every branch fixture
// under prefix converges on. Two prefixes therefore address two independent
// tables, which is what keeps the two live proofs from reading each other's
// seeded rows.
func sqlBlastRadiusTableFor(prefix string) string { return prefix + "_orders" }

// sqlBlastRadiusBranches enumerates every UNION branch in
// blastRadiusSqlTableCypher. blastRadiusSqlTableBranches is the compile-time
// count that must match, so a branch added to the query without a fixture here
// fails TestSQLTableBlastRadiusBranchTableMatchesQuery rather than going
// quietly unproven.
func sqlBlastRadiusBranches() []sqlBlastRadiusBranch {
	return sqlBlastRadiusBranchesFor(sqlBlastRadiusPrefix)
}

// sqlBlastRadiusBranchesFor builds the same nine fixtures under an arbitrary
// prefix. The bite proof (#6204) seeds eight of them under its own prefix and
// asserts the ninth is reported missing, which it can only do against fixtures
// that do not collide with the shipped set.
func sqlBlastRadiusBranchesFor(prefix string) []sqlBlastRadiusBranch {
	table := sqlBlastRadiusTableFor(prefix)
	repo := func(suffix string) string { return prefix + "-" + suffix }
	// Each seed hangs its own File off its own Repository, then links the
	// branch-specific node to the one shared SqlTable.
	chain := func(repoID, label, nodeName, edge string) string {
		return `MERGE (r:Repository {id: '` + repoID + `', name: '` + repoID + `'})
MERGE (f:File {path: '/` + repoID + `/schema.sql', repo_id: '` + repoID + `'})
MERGE (r)-[:REPO_CONTAINS]->(f)
MERGE (n:` + label + ` {name: '` + nodeName + `', repo_id: '` + repoID + `'})
MERGE (f)-[:CONTAINS]->(n)
WITH n
MATCH (t:SqlTable {name: '` + table + `'})
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
MERGE (t:SqlTable {name: '` + table + `', repo_id: '` + repo("contains") + `'})
MERGE (f)-[:CONTAINS]->(t)`,
		},
		{Name: "QUERIES_TABLE", RepoID: repo("queries"), Hops: 1, Seed: chain(repo("queries"), "Function", prefix+"_reader", "QUERIES_TABLE")},
		{Name: "TRIGGERS", RepoID: repo("triggers"), Hops: 1, Seed: chain(repo("triggers"), "SqlTrigger", prefix+"_trg", "TRIGGERS")},
		{Name: "INDEXES", RepoID: repo("indexes"), Hops: 1, Seed: chain(repo("indexes"), "SqlIndex", prefix+"_idx", "INDEXES")},
		{Name: "READS_FROM/SqlView", RepoID: repo("view"), Hops: 1, Seed: chain(repo("view"), "SqlView", prefix+"_view", "READS_FROM")},
		{Name: "READS_FROM/SqlFunction", RepoID: repo("fnread"), Hops: 1, Seed: chain(repo("fnread"), "SqlFunction", prefix+"_fnread", "READS_FROM")},
		{Name: "WRITES_TO/SqlFunction", RepoID: repo("fnwrite"), Hops: 1, Seed: chain(repo("fnwrite"), "SqlFunction", prefix+"_fnwrite", "WRITES_TO")},
		{Name: "REFERENCES_TABLE", RepoID: repo("fk"), Hops: 1, Seed: chain(repo("fk"), "SqlTable", prefix+"_child", "REFERENCES_TABLE")},
		{Name: "MIGRATES", RepoID: repo("migration"), Hops: 1, Seed: chain(repo("migration"), "SqlMigration", prefix+"_mig", "MIGRATES")},
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

// sqlBlastRadiusBackend resolves the live graph endpoint for both live tests,
// preferring each variable's ESHU_-prefixed form over the bare NEO4J_ name.
//
// The URI already worked that way; the database did not, and read
// NEO4J_DATABASE alone. scripts/verify-replay-tier.sh pins
// ESHU_NEO4J_DATABASE, so on a developer machine that already exports
// NEO4J_DATABASE=neo4j these tests connected to a different database than the
// replay tier had just asserted against, inside the same gate run. CI was
// unaffected — a clean runner leaves both unset and the nornic default holds —
// which is why it went unnoticed until #6201 review. The gate now pins both
// names as well; either fix alone would close the hole, and the pair keeps a
// future test that reads only one of them honest.
func sqlBlastRadiusBackend() (uri, database string) {
	firstNonEmpty := func(names ...string) string {
		for _, name := range names {
			if value := strings.TrimSpace(os.Getenv(name)); value != "" {
				return value
			}
		}
		return ""
	}

	uri = firstNonEmpty("ESHU_NEO4J_URI", "NEO4J_URI")
	if uri == "" {
		uri = "bolt://localhost:7687"
	}
	database = firstNonEmpty("ESHU_NEO4J_DATABASE", "NEO4J_DATABASE")
	if database == "" {
		database = "nornic"
	}
	return uri, database
}

// sqlBlastRadiusLiveReader opens the gate's graph endpoint and returns a reader
// bound to it, closing the driver through t.Cleanup rather than defer.
//
// The ordering is load-bearing and cost a real bug. With `defer
// driver.Close(...)` the driver closed as the test function returned, before
// any t.Cleanup callback, so every trailing delete failed with "Trying to
// create session on closed driver" and left probe nodes behind in the graph
// this gate shares with the replay tier (#6182). Registering the close FIRST
// puts it LAST in LIFO order, after whatever cleanup the caller registers next.
func sqlBlastRadiusLiveReader(ctx context.Context, t *testing.T) *Neo4jReader {
	t.Helper()
	uri, database := sqlBlastRadiusBackend()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	return NewNeo4jReader(driver, database)
}

// sqlBlastRadiusCleanup deletes every node a prefix's fixtures created plus the
// shared table they converge on. It runs on its own context because the test's
// context may already be cancelled by the time cleanup fires. Callers run it
// before seeding as well as after, so a crashed earlier run cannot answer this
// one's query.
func sqlBlastRadiusCleanup(t *testing.T, reader *Neo4jReader, prefix string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, stmt := range []string{
		`MATCH (n) WHERE n.repo_id STARTS WITH '` + prefix + `' DETACH DELETE n`,
		`MATCH (r:Repository) WHERE r.id STARTS WITH '` + prefix + `' DETACH DELETE r`,
		`MATCH (t:SqlTable {name: '` + sqlBlastRadiusTableFor(prefix) + `'}) DETACH DELETE t`,
	} {
		if _, err := reader.Run(ctx, stmt, nil); err != nil {
			t.Logf("cleanup %q: %v", stmt, err)
		}
	}
}

// sqlBlastRadiusMissingBranches reduces the rows blastRadiusSqlTableQuery
// ACTUALLY RETURNED down to the branch names that contributed no repository,
// and the lowest hop count each contributing repository came back at.
//
// This accumulation IS the dead-branch detection, and it must derive from rows
// and never from branches. A version that walked the fixture list would report
// nothing missing even if every shipped UNION branch were dead -- precisely the
// defect #6201 found in the test then named
// TestSQLTableBlastRadiusDetectsADeadBranchLive.
//
// It lives here, called by both the nine-branch proof and the bite proof
// (#6204), so the bite proof guards the code the nine-branch proof runs rather
// than a copy of it. Gutting this function to walk `branches` turns the bite
// proof red; that is the standing guard the manual INDEXES experiment in
// docs/internal/evidence/6182-blast-radius-gate-wiring.md used to stand in for.
func sqlBlastRadiusMissingBranches(
	rows []map[string]any,
	branches []sqlBlastRadiusBranch,
	prefix string,
) (missing []string, hops map[string]int64) {
	hops = map[string]int64{}
	for _, row := range rows {
		repoID, _ := row["repo_id"].(string)
		if !strings.HasPrefix(repoID, prefix) {
			continue
		}
		rowHops, _ := row["hops"].(int64)
		// Keep the smallest hop count a repo appears at, matching how the
		// handler collapses a repo that several branches reach.
		if prev, seen := hops[repoID]; !seen || rowHops < prev {
			hops[repoID] = rowHops
		}
	}
	for _, branch := range branches {
		if _, ok := hops[branch.RepoID]; !ok {
			missing = append(missing, branch.Name)
		}
	}
	sort.Strings(missing)
	return missing, hops
}

// TestSQLTableBlastRadiusEveryBranchContributesLive drives the real
// blastRadiusSqlTableQuery against a live NornicDB and asserts each of the nine
// UNION branches independently contributes its own repository at its own hop
// count.
func TestSQLTableBlastRadiusEveryBranchContributesLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPLAY_TIER_LIVE")) != "1" {
		t.Skip("set ESHU_REPLAY_TIER_LIVE=1 to run the sql_table blast-radius branch proof against a real NornicDB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	reader := sqlBlastRadiusLiveReader(ctx, t)

	branches := sqlBlastRadiusBranches()
	cleanup := func() { sqlBlastRadiusCleanup(t, reader, sqlBlastRadiusPrefix) }
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
		map[string]any{"target_name": sqlBlastRadiusTableFor(sqlBlastRadiusPrefix), "limit": 200})
	if err != nil {
		t.Fatalf("run blast radius: %v", err)
	}

	missing, got := sqlBlastRadiusMissingBranches(rows, branches, sqlBlastRadiusPrefix)
	for _, branch := range branches {
		hops, ok := got[branch.RepoID]
		if !ok {
			continue
		}
		if hops != branch.Hops {
			t.Errorf("branch %q: hops = %d, want %d", branch.Name, hops, branch.Hops)
		}
	}
	if len(missing) > 0 {
		t.Errorf("these UNION branches contributed NO repository: %v -- each branch has its own "+
			"repository precisely so a dead branch cannot hide behind its siblings; a branch here is "+
			"either not matching or has been broken by a query change", missing)
	}

	if len(got) != len(branches) {
		t.Errorf("blast radius returned %d seeded repositories, want %d: %v", len(got), len(branches), got)
	}
}

// TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive is a NEGATIVE
// CONTROL, not a bite proof. It drives the same query against a table nothing
// references and asserts no seeded repository comes back, which rules out the
// opposite failure from a dead branch: a branch matching everything, under
// which the positive proof above would pass for the wrong reason.
//
// It was named TestSQLTableBlastRadiusDetectsADeadBranchLive and documented as
// the bite proof, and it is neither. Its `missing` counter walks
// sqlBlastRadiusBranches() — a fixture list, never the query rows — so it
// passes even if every shipped UNION branch were dead. Both the codex reviewer
// and the repo owner caught that independently on #6201. The name mattered
// more than the test did: a future reader trusting it could delete the real
// per-branch proof and believe dead-branch detection was still enforced.
//
// Dead-branch detection lives in
// TestSQLTableBlastRadiusEveryBranchContributesLive, which accumulates the
// missing list from actual rows through sqlBlastRadiusMissingBranches. That the
// accumulation bites is now a standing test rather than the recorded manual
// experiment it was under #6182: see
// TestSQLTableBlastRadiusReportsUnseededBranchMissingLive (#6204), which seeds
// eight of the nine branches and asserts the ninth is reported missing by
// name.
func TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPLAY_TIER_LIVE")) != "1" {
		t.Skip("set ESHU_REPLAY_TIER_LIVE=1 to run the sql_table blast-radius negative control against a real NornicDB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reader := sqlBlastRadiusLiveReader(ctx, t)

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

	// The fixture-vs-constant count check that used to live here duplicated
	// TestSQLTableBlastRadiusBranchTableMatchesQuery, which already runs it
	// without a backend. Keeping it here dressed a compile-time comparison up as
	// live evidence, which is how this test came to be called a bite proof.
}
