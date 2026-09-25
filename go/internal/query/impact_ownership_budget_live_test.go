// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/impact/ownership"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Ownership-budget fixture shape. Every seeded id carries ownBudgetPrefix so the
// fixture deletes cleanly and never collides with another live test.
const (
	ownBudgetPrefix       = "own5167b:"
	ownBudgetRepos        = 256 // grant candidates; G is a prefix of these
	ownBudgetOwnedWIsEach = 2   // repo-owned WorkloadInstances per repo
	ownBudgetCRsPerWI     = 4   // CloudResources each owned WI USES
	ownBudgetTFsEach      = 8   // TerraformResource -MATCHES_STATE-> TSR pairs per repo
	ownBudgetDSWIsEach    = 8   // foreign WorkloadInstances with DEPLOYMENT_SOURCE to the repo
	ownBudgetNoiseTFs     = 20000
)

func ownBudgetID(kind string, repo, n int) string {
	return fmt.Sprintf("%s%s:%d:%d", ownBudgetPrefix, kind, repo, n)
}

// seedOwnershipBudgetFixture writes the scaled ownership fixture and returns
// the three classes' key lists (CloudResource uids, TSR uids, rescued WI ids).
func seedOwnershipBudgetFixture(t *testing.T, ctx context.Context, reader impactLiveReader) (crs, tsrs, wis []string) {
	t.Helper()
	exec := func(cypher string, params map[string]any) {
		t.Helper()
		if err := reader.ExecuteCypher(ctx, graph.CypherStatement{Cypher: cypher, Parameters: params}); err != nil {
			t.Fatalf("seed: %v\n%s", err, cypher)
		}
	}
	deleteOwnBudgetFixture(ctx, reader)

	var repos, owned, uses, tfs, dss []map[string]any
	for r := range ownBudgetRepos {
		repoID := ownBudgetID("repo", r, 0)
		repos = append(repos, map[string]any{"id": repoID})
		for w := range ownBudgetOwnedWIsEach {
			wi := ownBudgetID("wi", r, w)
			owned = append(owned, map[string]any{"id": wi, "repo_id": repoID})
			for c := range ownBudgetCRsPerWI {
				cr := ownBudgetID("cr", r, w*ownBudgetCRsPerWI+c)
				uses = append(uses, map[string]any{"wi": wi, "uid": cr})
				crs = append(crs, cr)
			}
		}
		for f := range ownBudgetTFsEach {
			tsr := ownBudgetID("tsr", r, f)
			tfs = append(tfs, map[string]any{"uid": ownBudgetID("tf", r, f), "repo_id": repoID, "tsr": tsr})
			tsrs = append(tsrs, tsr)
		}
		for d := range ownBudgetDSWIsEach {
			wi := ownBudgetID("dswi", r, d)
			dss = append(dss, map[string]any{"id": wi, "repo_id": ownBudgetPrefix + "foreign", "repo": repoID})
			wis = append(wis, wi)
		}
	}
	var noise []map[string]any
	for n := range ownBudgetNoiseTFs {
		noise = append(noise, map[string]any{"uid": ownBudgetID("noisetf", 0, n), "repo_id": ownBudgetPrefix + "noise"})
	}
	batch := func(cypher string, rows []map[string]any) {
		for start := 0; start < len(rows); start += 1000 {
			exec(cypher, map[string]any{"rows": rows[start:min(start+1000, len(rows))]})
		}
	}
	batch(`UNWIND $rows AS row CREATE (:Repository {id: row.id, name: row.id})`, repos)
	batch(`UNWIND $rows AS row CREATE (:WorkloadInstance {id: row.id, repo_id: row.repo_id})`, owned)
	batch(`UNWIND $rows AS row MATCH (wi:WorkloadInstance {id: row.wi}) CREATE (wi)-[:USES]->(:CloudResource {uid: row.uid, id: row.uid})`, uses)
	batch(`UNWIND $rows AS row CREATE (:TerraformResource {uid: row.uid, repo_id: row.repo_id})-[:MATCHES_STATE]->(:TerraformStateResource {uid: row.tsr, id: row.tsr})`, tfs)
	batch(`UNWIND $rows AS row MATCH (r:Repository {id: row.repo}) CREATE (:WorkloadInstance {id: row.id, repo_id: row.repo_id})-[:DEPLOYMENT_SOURCE]->(r)`, dss)
	batch(`UNWIND $rows AS row CREATE (:TerraformResource {uid: row.uid, repo_id: row.repo_id})`, noise)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		deleteOwnBudgetFixture(cctx, reader)
	})
	return crs, tsrs, wis
}

// deleteOwnBudgetFixture removes the fixture label by label in bounded,
// key-listed batches. One DETACH DELETE over the whole fixture is too large a
// transaction for NornicDB ("Txn is too big to fit into one request"), and
// `WITH n LIMIT k DETACH DELETE n` deletes nothing on the pinned build, so each
// batch reads up to 2000 keys and deletes exactly those.
func deleteOwnBudgetFixture(ctx context.Context, reader impactLiveReader) {
	for _, label := range []string{"CloudResource", "TerraformStateResource", "TerraformResource", "WorkloadInstance", "Repository"} {
		for range 500 {
			rows, err := reader.Run(ctx, `MATCH (n:`+label+`) WHERE n.id STARTS WITH $p OR n.uid STARTS WITH $p
RETURN coalesce(n.uid, n.id) AS key LIMIT 2000`, map[string]any{"p": ownBudgetPrefix})
			if err != nil || len(rows) == 0 {
				break
			}
			keys := make([]string, 0, len(rows))
			for _, row := range rows {
				keys = append(keys, querycontract.StringVal(row, "key"))
			}
			if _, err := reader.RunWrite(ctx, `UNWIND $keys AS k MATCH (n:`+label+`) WHERE n.uid = k OR n.id = k DETACH DELETE n`,
				map[string]any{"keys": keys}); err != nil {
				break
			}
		}
	}
}

func ownBudgetGrant(size int) []string {
	out := make([]string, size)
	for i := range size {
		out[i] = ownBudgetID("repo", i, 0)
	}
	return out
}

// grantAnchored* are the grant-anchored statement shapes the #5167 design
// first specified (A1, R1b keyed on id, R2). They are kept here only as the
// measured comparison that ruled them out; production uses the grant-free
// owner projections in impact/ownership/statements.go.
const (
	grantAnchoredCloudResource = `UNWIND $grant_ids AS g
MATCH (wi:WorkloadInstance {repo_id: g})-[:USES]->(n:CloudResource)
WHERE n.uid IN $uids
RETURN DISTINCT n.uid AS uid`
	grantAnchoredWorkloadInstance = `UNWIND $grant_ids AS g
MATCH (r:Repository {id: g})<-[:DEPLOYMENT_SOURCE]-(wi:WorkloadInstance)
WHERE wi.id IN $uids
RETURN DISTINCT wi.id AS uid`
	grantAnchoredTerraformState = `UNWIND $grant_ids AS g
MATCH (t:TerraformResource {repo_id: g})-[:MATCHES_STATE]->(n:TerraformStateResource)
WHERE n.uid IN $uids
RETURN DISTINCT n.uid AS uid`
)

// ownBudgetAccess is a scoped filter over the first size fixture repos plus
// filler ids up to total, so the grant is large while the owned set is known.
func ownBudgetAccess(size, total int) querycontract.RepositoryAccessFilter {
	ids := ownBudgetGrant(size)
	for i := size; i < total; i++ {
		ids = append(ids, fmt.Sprintf("%sfiller:%d", ownBudgetPrefix, i))
	}
	return querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: ids}
}

func ownBudgetNodes(label string, keys []string) []ownership.Node {
	nodes := make([]ownership.Node, 0, len(keys))
	for _, key := range keys {
		n := ownership.Node{ID: key, UID: key, Labels: []string{label}}
		if label == "WorkloadInstance" {
			n.UID, n.RepoID = "", ownBudgetPrefix+"foreign"
		}
		nodes = append(nodes, n)
	}
	return nodes
}

// ownBudgetOwned reports whether a fixture key (prefix + kind:repo:n) belongs
// to a repo index below size, i.e. whether a grant of the first size repos
// owns it.
func ownBudgetOwned(key string, size int) bool {
	parts := strings.Split(strings.TrimPrefix(key, ownBudgetPrefix), ":")
	if len(parts) != 3 {
		return false
	}
	var repo int
	if _, err := fmt.Sscanf(parts[1], "%d", &repo); err != nil {
		return false
	}
	return repo < size
}

// TestLiveImpactOwnershipStatementCost measures the shipped owner statements
// through ownership.Checker, uncached, over every fixture key of each class at
// grant sizes 8, 128, and 1000, and checks the admitted set against the
// fixture's truth. With ESHU_IMPACT_OWNERSHIP_COMPARE=1 it also measures the
// grant-anchored shapes the design first specified (slow: R2 is a label scan
// per grant id because TerraformResource.repo_id has no index). It is the
// measured basis of ownership.ChunkSize and ownership.MaxCheckedKeys.
//
//	Run: ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17998 \
//		go test ./internal/query -run TestLiveImpactOwnershipStatementCost -count=1 -v
func TestLiveImpactOwnershipStatementCost(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OCI_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OCI_PROVE_LIVE=1 to run the live ownership budget proof")
	}
	reader := openImpactLiveReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	crs, tsrs, wis := seedOwnershipBudgetFixture(t, ctx, reader)
	classes := []struct {
		label string
		keys  []string
	}{{"CloudResource", crs}, {"WorkloadInstance", wis}, {"TerraformStateResource", tsrs}}
	for round, grantSize := range []int{8, 128, 1000} {
		access := ownBudgetAccess(min(grantSize, ownBudgetRepos), grantSize)
		for _, class := range classes {
			// Rotate the key order each round so no chunk repeats an earlier
			// statement's params: this build caches read results, and a
			// repeated chunk would time a cache hit, not the statement.
			shift := round * 17
			keys := append(append([]string{}, class.keys[shift:]...), class.keys[:shift]...)
			nodes := ownBudgetNodes(class.label, keys)
			began := time.Now()
			verdict, err := ownership.Checker{Graph: reader, Route: "live-test"}.Check(ctx, access, nodes)
			elapsed := time.Since(began)
			if err != nil {
				t.Fatalf("%s G=%d: %v", class.label, grantSize, err)
			}
			admitted, wrong := 0, 0
			for _, n := range nodes {
				got := verdict.Admits(n)
				if got {
					admitted++
				}
				if got != ownBudgetOwned(n.ID, min(grantSize, ownBudgetRepos)) {
					wrong++
				}
			}
			t.Logf("MEASURE shipped class=%s G=%d U=%d chunk=%d elapsed=%.3fs admitted=%d wrong=%d",
				class.label, grantSize, len(nodes), ownership.ChunkSize, elapsed.Seconds(), admitted, wrong)
			if wrong != 0 {
				t.Errorf("%s G=%d: %d keys judged against the fixture truth", class.label, grantSize, wrong)
			}
		}
	}
	if strings.TrimSpace(os.Getenv("ESHU_IMPACT_OWNERSHIP_COMPARE")) == "" {
		return
	}
	compare := []struct {
		name, cypher string
		keys         []string
	}{
		{"CloudResource(A1)", grantAnchoredCloudResource, crs[:2000]},
		{"WorkloadInstance(R1b)", grantAnchoredWorkloadInstance, wis[:2000]},
		{"TerraformStateResource(R2)", grantAnchoredTerraformState, tsrs[:2000]},
	}
	for _, grantSize := range []int{8, 128} {
		for _, shape := range compare {
			for _, chunk := range []int{2000, 500} {
				began := time.Now()
				for start := 0; start < len(shape.keys); start += chunk {
					if _, err := reader.Run(ctx, shape.cypher, map[string]any{
						"grant_ids": ownBudgetGrant(grantSize), "uids": shape.keys[start:min(start+chunk, len(shape.keys))],
						"nonce": fmt.Sprintf("%d", time.Now().UnixNano()),
					}); err != nil {
						t.Fatalf("%s: %v", shape.name, err)
					}
				}
				t.Logf("MEASURE grant-anchored class=%s G=%d U=%d chunk=%d elapsed=%.3fs", shape.name, grantSize, len(shape.keys), chunk, time.Since(began).Seconds())
			}
		}
	}
}

// TestLiveImpactOwnershipCapAndDeadline is T7: at a 1000-id grant, a page of
// every fixture key (6144 statement-checked keys, over MaxCheckedKeys) is
// chunked, capped, judged correctly up to the cap, reported capped (so the
// route reports truncated), and finishes far inside the 10 s graph-read
// deadline. A page at the cap is not capped.
func TestLiveImpactOwnershipCapAndDeadline(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OCI_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OCI_PROVE_LIVE=1 to run the live ownership cap proof")
	}
	reader := openImpactLiveReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	crs, tsrs, wis := seedOwnershipBudgetFixture(t, ctx, reader)
	access := ownBudgetAccess(ownBudgetRepos, 1000)
	var page []ownership.Node
	page = append(page, ownBudgetNodes("CloudResource", crs)...)
	page = append(page, ownBudgetNodes("WorkloadInstance", wis)...)
	page = append(page, ownBudgetNodes("TerraformStateResource", tsrs)...)
	paths := make([][]ownership.Node, len(page))
	for i, n := range page {
		paths[i] = []ownership.Node{n}
	}

	checker := ownership.Checker{Graph: reader, Route: "live-test"}
	began := time.Now()
	filter, err := checker.FilterPaths(ctx, access, paths)
	elapsed := time.Since(began)
	if err != nil {
		t.Fatalf("FilterPaths: %v", err)
	}
	t.Logf("MEASURE cap G=1000 keys=%d cap=%d kept=%d capped=%v elapsed=%.3fs", len(page), ownership.MaxCheckedKeys, filter.Kept(), filter.Capped, elapsed.Seconds())
	if !filter.Capped {
		t.Fatalf("a %d-key page over the %d cap was not reported capped", len(page), ownership.MaxCheckedKeys)
	}
	if filter.Kept() != ownership.MaxCheckedKeys {
		t.Fatalf("kept %d paths, want every checked key (all owned at this grant) = %d", filter.Kept(), ownership.MaxCheckedKeys)
	}
	for i := ownership.MaxCheckedKeys; i < len(page); i++ {
		if filter.Keep[i] {
			t.Fatalf("path %d past the cap was kept; unchecked keys must be ungranted", i)
		}
	}
	if elapsed > 5*time.Second {
		t.Fatalf("capped check took %s, want well under the 10 s graph-read deadline", elapsed)
	}

	atCap := paths[:ownership.MaxCheckedKeys]
	filter, err = checker.FilterPaths(ctx, access, atCap)
	if err != nil {
		t.Fatalf("FilterPaths at cap: %v", err)
	}
	if filter.Capped || filter.Kept() != len(atCap) {
		t.Fatalf("a page at the cap: capped=%v kept=%d, want uncapped and all %d kept", filter.Capped, filter.Kept(), len(atCap))
	}
}

// TestLiveImpactOwnershipGrantFilterVariant measures the keyed-node-first
// shapes with the grant as a WHERE filter and a hard row LIMIT, against
// fixture truth, at grant 8, 128, and 1000 (uncached: key order rotates).
func TestLiveImpactOwnershipGrantFilterVariant(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OCI_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OCI_PROVE_LIVE=1")
	}
	reader := openImpactLiveReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	crs, tsrs, wis := seedOwnershipBudgetFixture(t, ctx, reader)
	shapes := []struct {
		name, cypher string
		keys         []string
	}{
		{"CloudResource", `MATCH (n:CloudResource)<-[:USES]-(wi:WorkloadInstance)
WHERE n.uid IN $uids AND wi.repo_id IN $grant_ids
RETURN DISTINCT n.uid AS uid, wi.repo_id AS repo_id
ORDER BY uid, repo_id
LIMIT $row_limit`, crs},
		{"WorkloadInstance", `MATCH (wi:WorkloadInstance)-[:DEPLOYMENT_SOURCE]->(r:Repository)
WHERE wi.id IN $uids AND r.id IN $grant_ids
RETURN DISTINCT wi.id AS uid, r.id AS repo_id
ORDER BY uid, repo_id
LIMIT $row_limit`, wis},
		{"TerraformStateResource", `MATCH (n:TerraformStateResource)<-[:MATCHES_STATE]-(t:TerraformResource)
WHERE n.uid IN $uids AND t.repo_id IN $grant_ids
RETURN DISTINCT n.uid AS uid, t.repo_id AS repo_id
ORDER BY uid, repo_id
LIMIT $row_limit`, tsrs},
	}
	for round, grantSize := range []int{8, 128, 1000} {
		size := min(grantSize, ownBudgetRepos)
		grant := ownBudgetAccess(size, grantSize).AllowedRepositoryIDs
		for _, shape := range shapes {
			shift := 7 + round*13
			keys := append(append([]string{}, shape.keys[shift:]...), shape.keys[:shift]...)
			admitted := map[string]bool{}
			began := time.Now()
			for start := 0; start < len(keys); start += 50 {
				rows, err := reader.Run(ctx, shape.cypher, map[string]any{"uids": keys[start:min(start+50, len(keys))], "grant_ids": grant, "row_limit": 800})
				if err != nil {
					t.Fatalf("%s: %v", shape.name, err)
				}
				for _, row := range rows {
					admitted[querycontract.StringVal(row, "uid")] = true
				}
			}
			elapsed := time.Since(began)
			wrong := 0
			for _, key := range keys {
				if admitted[key] != ownBudgetOwned(key, size) {
					wrong++
				}
			}
			t.Logf("MEASURE grant-filter class=%s G=%d U=%d chunk=50 elapsed=%.3fs admitted=%d wrong=%d", shape.name, grantSize, len(keys), elapsed.Seconds(), len(admitted), wrong)
		}
	}
}
