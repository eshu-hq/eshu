// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/impact/ownership"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Hub fan-in fixture (#5167 review B3). Every hub CloudResource is USED by
// ownHubFanIn WorkloadInstances, each owned by a distinct repository, so one
// 50-key chunk expands ownHubs*ownHubFanIn USES edges and distinct
// (uid, repo_id) pairs before DISTINCT, ORDER BY, and LIMIT. The plain keys
// have one owner each, the fan-in the original budget measured.
const (
	ownHubPrefix = "own5167h:"
	ownHubs      = 50
	ownHubFanIn  = 2000
	ownHubPlain  = ownership.MaxCheckedKeys - ownHubs
	ownHubRuns   = 9
)

func ownHubKey(kind string, n int) string { return fmt.Sprintf("%s%s:%05d", ownHubPrefix, kind, n) }

// ownHubRepo is the repository owning a hub's w-th WorkloadInstance. It is
// zero-padded so ORDER BY repo_id is also numeric order.
func ownHubRepo(w int) string { return ownHubKey("repo", w) }

func seedOwnershipHubFixture(t *testing.T, ctx context.Context, reader impactLiveReader) (hubs, plain []string) {
	t.Helper()
	deleteOwnHubFixture(ctx, reader)
	batch := func(cypher string, rows []map[string]any) {
		t.Helper()
		for start := 0; start < len(rows); start += 1000 {
			if err := reader.ExecuteCypher(ctx, graph.CypherStatement{Cypher: cypher, Parameters: map[string]any{
				"rows": rows[start:min(start+1000, len(rows))],
			}}); err != nil {
				t.Fatalf("seed hub fixture: %v", err)
			}
		}
	}
	var crs, wis []map[string]any
	for h := range ownHubs {
		hub := ownHubKey("hub", h)
		hubs = append(hubs, hub)
		crs = append(crs, map[string]any{"uid": hub})
		for w := range ownHubFanIn {
			wis = append(wis, map[string]any{"id": ownHubKey(fmt.Sprintf("hubwi%d", h), w), "repo_id": ownHubRepo(w), "cr": hub})
		}
	}
	for p := range ownHubPlain {
		key := ownHubKey("plain", p)
		plain = append(plain, key)
		crs = append(crs, map[string]any{"uid": key})
		wis = append(wis, map[string]any{"id": ownHubKey("plainwi", p), "repo_id": ownHubRepo(p % ownHubFanIn), "cr": key})
	}
	batch(`UNWIND $rows AS row CREATE (:CloudResource {uid: row.uid, id: row.uid})`, crs)
	batch(`UNWIND $rows AS row MATCH (cr:CloudResource {uid: row.cr}) CREATE (:WorkloadInstance {id: row.id, repo_id: row.repo_id})-[:USES]->(cr)`, wis)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		deleteOwnHubFixture(cctx, reader)
	})
	return hubs, plain
}

func deleteOwnHubFixture(ctx context.Context, reader impactLiveReader) {
	for _, label := range []string{"WorkloadInstance", "CloudResource"} {
		for range 500 {
			rows, err := reader.Run(ctx, `MATCH (n:`+label+`) WHERE n.id STARTS WITH $p RETURN n.id AS key LIMIT 2000`,
				map[string]any{"p": ownHubPrefix})
			if err != nil || len(rows) == 0 {
				break
			}
			keys := make([]string, 0, len(rows))
			for _, row := range rows {
				keys = append(keys, querycontract.StringVal(row, "key"))
			}
			if _, err := reader.RunWrite(ctx, `UNWIND $keys AS k MATCH (n:`+label+` {id: k}) DETACH DELETE n`,
				map[string]any{"keys": keys}); err != nil {
				break
			}
		}
	}
}

func durationStats(samples []time.Duration) (median, maxD time.Duration) {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2], sorted[len(sorted)-1]
}

// TestLiveImpactOwnershipHubFanIn measures the shipped CloudResource owner
// statement on a chunk of 50 hub keys (ownHubFanIn owners each), uncached
// (a unique $nonce per run), and a full MaxCheckedKeys page holding those hubs
// among fan-in-1 keys through Checker. It also proves the admit decision at a
// hub: a granted owner the RowLimit cut off leaves the key unchecked (so the
// route reports truncated), never admitted.
//
//	Run: ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17998 \
//		go test ./internal/query -run TestLiveImpactOwnershipHubFanIn -count=1 -v
func TestLiveImpactOwnershipHubFanIn(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OCI_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OCI_PROVE_LIVE=1 to run the live ownership hub fan-in proof")
	}
	reader := openImpactLiveReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	seedBegan := time.Now()
	hubs, plain := seedOwnershipHubFixture(t, ctx, reader)
	t.Logf("seeded hubs=%d fan_in=%d plain=%d in %.1fs", len(hubs), ownHubFanIn, len(plain), time.Since(seedBegan).Seconds())

	// Per-chunk cost: the shipped statement over the 50 hub keys.
	samples := make([]time.Duration, 0, ownHubRuns)
	for run := range ownHubRuns {
		began := time.Now()
		rows, err := reader.Run(ctx, ownership.CloudResourceOwnerCypher, map[string]any{
			"uids": hubs, "row_limit": ownership.RowLimit, "nonce": fmt.Sprintf("hub-%d-%d", run, time.Now().UnixNano()),
		})
		elapsed := time.Since(began)
		if err != nil {
			t.Fatalf("hub chunk run %d: %v", run, err)
		}
		samples = append(samples, elapsed)
		t.Logf("MEASURE hub-chunk run=%d keys=%d fan_in=%d rows=%d elapsed=%.3fs", run, len(hubs), ownHubFanIn, len(rows), elapsed.Seconds())
	}
	median, maxD := durationStats(samples)
	t.Logf("MEASURE hub-chunk summary n=%d median=%.3fs max=%.3fs", len(samples), median.Seconds(), maxD.Seconds())

	// Full page: the hubs spread through MaxCheckedKeys keys, key order
	// rotated per run so no chunk repeats an earlier run's parameters.
	page := append([]string{}, plain...)
	for i, hub := range hubs {
		at := min(i*(len(page)/len(hubs)), len(page))
		page = append(page[:at], append([]string{hub}, page[at:]...)...)
	}
	pageSamples := make([]time.Duration, 0, ownHubRuns)
	lastGrant := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{ownHubRepo(ownHubFanIn - 1)}}
	for run := range ownHubRuns {
		shift := run * 7
		keys := append(append([]string{}, page[shift:]...), page[:shift]...)
		nodes := ownBudgetNodes("CloudResource", keys)
		began := time.Now()
		verdict, err := ownership.Checker{Graph: reader, Route: "live-test"}.Check(ctx, lastGrant, nodes)
		elapsed := time.Since(began)
		if err != nil {
			t.Fatalf("page run %d: %v", run, err)
		}
		pageSamples = append(pageSamples, elapsed)
		admittedHubs := 0
		for _, hub := range hubs {
			if verdict.Admits(ownership.Node{ID: hub, UID: hub, Labels: []string{"CloudResource"}}) {
				admittedHubs++
			}
		}
		t.Logf("MEASURE hub-page run=%d keys=%d hubs=%d elapsed=%.3fs capped=%v admitted_hubs=%d", run, len(nodes), len(hubs), elapsed.Seconds(), verdict.Capped(), admittedHubs)
		if !verdict.Capped() {
			t.Fatalf("page run %d: a hub chunk past RowLimit must report Capped", run)
		}
		// The granted owner sorts last, past RowLimit: every hub stays
		// undecided (ungranted, truncated), never admitted.
		if admittedHubs != 0 {
			t.Fatalf("page run %d: %d hubs admitted though their granted owner was cut by RowLimit", run, admittedHubs)
		}
	}
	median, maxD = durationStats(pageSamples)
	t.Logf("MEASURE hub-page summary n=%d median=%.3fs max=%.3fs", len(pageSamples), median.Seconds(), maxD.Seconds())
	if maxD > 10*time.Second {
		t.Fatalf("hub page took %s, over the 10 s graph-read deadline", maxD)
	}
}
