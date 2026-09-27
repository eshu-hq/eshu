// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// #7339 high-degree-tail repo-scale live proof. The #7325 record
// (docs/internal/evidence/7325-outlier-fanout-batch.md, Proof limits /
// NOT_CHECKED) measured the outlier CALLS fan-out's committed 250-key
// batch only against seeds whose maximum out-degree is 64 -- far below
// the ops-qa tail (max out-degree 521,
// docs/internal/evidence/6649-calls-degree-floor.md) and the registry
// audit bound (250 keys x corpus CALLS degree floor 1125 = 281,250 rows
// from one statement). This file reuses the #6929/#7297 base seed plus
// the #7325 density seed, then adds a deterministic high-degree tail
// (max 521) aligned so production chunk index 1 of the sorted cohort
// member list is exactly 250 high-degree members, and proves batch 50
// vs. 250 return byte-identical rows and findings at that tail, in both
// key orders and both batch-execution orders, with cold and warm p95.
// Backend is Neo4j only; NornicDB is not a target.
//
// Run against a fresh pinned container per run (unique nonce per run;
// never reuse a container across runs, since the seed is CREATE-based
// and not idempotent):
//
//	nonce=$(head -c4 /dev/urandom | od -An -tx1 | tr -d ' \n')
//	docker run -d --name eshu-7339-neo4j-$nonce -p 17439:7687 \
//	  -e NEO4J_AUTH=neo4j/eshu-7339-pass \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	ESHU_OUTLIER_NEO4J_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17439 \
//	  ESHU_NEO4J_USER=neo4j ESHU_NEO4J_PASSWORD=eshu-7339-pass \
//	  ESHU_RUN_NONCE=$nonce \
//	  go test ./internal/query/codequery -run TestLiveOutlierTailRepoScaleNeo4j -v -count=1 -timeout 20m
//	docker rm -f eshu-7339-neo4j-$nonce
package codequery

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// tailChunkSize is the production 250-key chunk this proof aligns the
// high-degree tail to: tailSources (below) is exactly the sorted cohort
// member list's chunk index 1, so the native 250-key batch reads the
// whole tail in one statement while the effective 50-key batch reads it
// in five.
const tailChunkSize = 250

// tailDegreeForChunkRank returns the CALLS out-degree seeded for rank j
// (0..249) of the tail chunk, deterministically shaped to the ops-qa
// reference tail (max out-degree 521, #6649): one anchor at the max, a
// short taper, and a long 25-degree body. No math/rand, for
// reproducibility.
func tailDegreeForChunkRank(j int) int {
	switch {
	case j == 0:
		return 521
	case j == 1:
		return 400
	case j < 4:
		return 300
	case j < 10:
		return 200
	case j < 30:
		return 100
	case j < 70:
		return 50
	default:
		return 25
	}
}

// tailSeedCallsEdges adds the deterministic high-degree tail among the
// swept repository's members: tailSources (exactly production chunk
// index 1 of the sorted cohort member list, all noise members) get
// tailDegreeForChunkRank out-degree via MERGE (matching the ops-qa
// MERGE-dedup methodology in #6649, so per-node relationship count
// equals distinct-callee out-degree). Targets spread deterministically
// across all noise members with per-source dedup. Returns the number of
// MERGE rows attempted; the measured max comes from tailChunkStats.
func tailSeedCallsEdges(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext, tailSources []string) int {
	t.Helper()

	uids := densityNoiseFunctionUIDs()
	n := len(uids)
	type edgeRow struct{ From, To string }
	rows := make([]edgeRow, 0, 12000)
	attempted := 0
	for j, from := range tailSources {
		degree := tailDegreeForChunkRank(j)
		used := map[int]struct{}{}
		for k := 0; len(used) < degree; k++ {
			// Deterministic pseudo-spread with a tail-only stride salt
			// (777) so targets do not correlate with the density seed's
			// stride; linear probing on collision keeps per-source
			// targets distinct without math/rand.
			targetIdx := (j*7919 + k*104729 + 777) % n
			for {
				if _, dup := used[targetIdx]; !dup && uids[targetIdx] != from {
					break
				}
				targetIdx = (targetIdx + 1) % n
			}
			used[targetIdx] = struct{}{}
			rows = append(rows, edgeRow{From: from, To: uids[targetIdx]})
			attempted++
		}
	}
	t.Logf("seeding %d tail CALLS edges over %d high-degree members (max degree %d)", attempted, len(tailSources), tailDegreeForChunkRank(0))

	cypher := `
		UNWIND $rows AS row
		MATCH (from:Function {uid: row.from})
		MATCH (to:Function {uid: row.to})
		MERGE (from)-[r:CALLS]->(to)
		SET r.resolution_method = "declared", r.confidence = 0.9
	`
	batch := make([]map[string]any, 0, liveScaleWriteBatch)
	flush := func(idx int) {
		if len(batch) == 0 {
			return
		}
		liveScaleRunWrite(ctx, t, session, fmt.Sprintf("seed tail CALLS batch %d", idx), cypher,
			map[string]any{"rows": batch})
		batch = batch[:0]
	}
	batchIdx := 0
	for _, row := range rows {
		batch = append(batch, map[string]any{"from": row.From, "to": row.To})
		if len(batch) >= liveScaleWriteBatch {
			flush(batchIdx)
			batchIdx++
		}
	}
	flush(batchIdx)
	return attempted
}

// tailChunkStats queries the seeded out-degree distribution over exactly
// the tail sources directly from Neo4j (measured, not assumed) and
// reports mean/p50/p90/max. It fails the test when the tail anchor is
// missing (max < 500): without the 521-degree member this proof
// exercises nothing beyond #7325's max-64 seed.
func tailChunkStats(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext, tailSources []string) {
	t.Helper()

	result, err := session.Run(ctx, `
		UNWIND $ids AS id
		MATCH (f:Function {uid: id})
		OPTIONAL MATCH (f)-[r:CALLS]->()
		WITH f, count(r) AS deg
		RETURN deg
		ORDER BY deg
	`, map[string]any{"ids": tailSources})
	if err != nil {
		t.Fatalf("tail chunk stats: %v", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatalf("tail chunk stats collect: %v", err)
	}
	if len(records) != len(tailSources) {
		t.Fatalf("tail chunk stats rows = %d, want %d (every tail source must resolve)", len(records), len(tailSources))
	}
	degrees := make([]int64, 0, len(records))
	var sum int64
	for _, rec := range records {
		v, _ := rec.Get("deg")
		d, _ := v.(int64)
		degrees = append(degrees, d)
		sum += d
	}
	pct := func(p float64) int64 {
		return degrees[int(p*float64(len(degrees)-1))]
	}
	mean := float64(sum) / float64(len(degrees))
	maxDeg := degrees[len(degrees)-1]
	t.Logf("measured tail-chunk out-degree over %d members: mean=%.1f p50=%d p90=%d max=%d",
		len(degrees), mean, pct(0.50), pct(0.90), maxDeg)
	if maxDeg < 500 {
		t.Fatalf("tail chunk max out-degree = %d, want >= 500 (the 521-degree anchor is missing)", maxDeg)
	}
}

// tailSummarize sorts ascending and returns cold (first sample),
// warm p50, warm p95, and warm max over runs[1:].
func tailSummarize(runs []time.Duration) (cold, p50, p95, maxD time.Duration) {
	cold = runs[0]
	warm := append([]time.Duration(nil), runs[1:]...)
	sort.Slice(warm, func(i, j int) bool { return warm[i] < warm[j] })
	return cold, liveScalePercentile(warm, 0.5), liveScalePercentile(warm, 0.95), warm[len(warm)-1]
}

// TestLiveOutlierTailRepoScaleNeo4j is the #7339 high-degree-tail proof:
// seed the #6929/#7297 base plus the #7325 density shape plus a 521-max
// tail aligned to production chunk index 1, then run the real production
// path at the committed 250-key batch and at an effective 50-key batch
// (via subBatchSplittingReader) in both key orders and both
// batch-execution orders, asserting byte-identical rows and findings and
// logging cold/warm-p50/warm-p95 for the tail single-statement probe,
// the whole-cohort fan-out, and the full sweep.
func TestLiveOutlierTailRepoScaleNeo4j(t *testing.T) {
	liveScaleSkipUnset(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	nonce := strings.TrimSpace(os.Getenv("ESHU_RUN_NONCE"))
	t.Logf("run nonce: %q (unique container per run; empty means the caller did not pass one)", nonce)

	driver := liveScaleOpenDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()

	const database = "neo4j"
	liveScaleApplySchema(ctx, t, driver, database)

	writeSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	seedStarted := time.Now()
	targetSeeded, otherSeeded := liveScaleSeedGraph(ctx, t, writeSession)
	t.Logf("base seed: target=%d other=%d in %s", targetSeeded, otherSeeded, time.Since(seedStarted))
	edgeStarted := time.Now()
	edgeCount := densitySeedCallsEdges(ctx, t, writeSession)
	t.Logf("density CALLS edges: %d in %s", edgeCount, time.Since(edgeStarted))
	_ = writeSession.Close(ctx)

	// Enumerate the cohort members to fix the tail alignment: the tail
	// is exactly sorted-chunk index 1 (all noise members; chunk 0 holds
	// the 11 s6929:-prefixed signal members first, so chunk 1 avoids
	// perturbing the signal fixture's majority-callee math).
	probeHandler := liveScaleAuthedHandler(driver, database)
	seeds, err := probeHandler.readOutlierCohortSeeds(ctx, liveScaleRepo, outlierCohortSources())
	if err != nil {
		t.Fatalf("readOutlierCohortSeeds: %v", err)
	}
	cohorts, _ := codedivergence.GroupOutlierCohorts(seeds, codedivergence.DefaultOutlierParams())
	memberSet := map[string]struct{}{}
	for _, cohort := range cohorts {
		for _, member := range cohort.Members {
			memberSet[member] = struct{}{}
		}
	}
	memberIDs := make([]string, 0, len(memberSet))
	for id := range memberSet {
		memberIDs = append(memberIDs, id)
	}
	sort.Strings(memberIDs)
	t.Logf("cohort member count for tail comparison: %d", len(memberIDs))
	if len(memberIDs) < 2*tailChunkSize {
		t.Fatalf("cohort members = %d, want >= %d to align the tail to chunk index 1", len(memberIDs), 2*tailChunkSize)
	}
	tailSources := append([]string(nil), memberIDs[tailChunkSize:2*tailChunkSize]...)
	for _, id := range tailSources {
		if strings.HasPrefix(id, liveScaleSignalPrefix+":") {
			t.Fatalf("tail source %q hits the signal fixture; chunk-1 alignment assumption is broken", id)
		}
	}

	tailWriteSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	tailStarted := time.Now()
	tailAttempted := tailSeedCallsEdges(ctx, t, tailWriteSession, tailSources)
	t.Logf("tail CALLS edges attempted: %d in %s", tailAttempted, time.Since(tailStarted))
	_ = tailWriteSession.Close(ctx)

	readSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = readSession.Close(ctx) }()

	densityDegreeStats(ctx, t, readSession)
	tailChunkStats(ctx, t, readSession, tailSources)

	// EXPLAIN the production tail statement shape (log-only: the anchor
	// and index are unchanged from #7325, only the data distribution is
	// new; phase-1 PROFILE already pinned the NodeUniqueIndexSeek plan).
	tailCypher, tailParams := BuildOutlierCalleeEdgesCypher(tailSources, liveScaleRepo,
		probeHandler.graphBackend(), codeGrantAccessFilter(ctx))
	t.Logf("tail statement plan operators: %v", liveScaleExplain(ctx, t, readSession, "tail fan-out", tailCypher, tailParams))

	baseReader := newLiveNornicDBReader(driver, database)
	handler250 := liveScaleAuthedHandler(driver, database) // native production 250-key batch
	handler50 := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNeo4j,
		Neo4j:        subBatchSplittingReader{inner: baseReader, subBatch: 50},
		Content:      outlierFixtureStore{membersByID: liveScaleMembersByID()},
	}

	descMembers := append([]string(nil), memberIDs...)
	for i, j := 0, len(descMembers)-1; i < j; i, j = i+1, j-1 {
		descMembers[i], descMembers[j] = descMembers[j], descMembers[i]
	}
	descTail := append([]string(nil), tailSources...)
	for i, j := 0, len(descTail)-1; i < j; i, j = i+1, j-1 {
		descTail[i], descTail[j] = descTail[j], descTail[i]
	}

	type cell struct {
		label                           string
		tailP95, tailMax                time.Duration
		tailHash, edgeHash, findingHash string
		findingCount                    int
	}
	cells := make([]cell, 0, 4)
	// Both batch-execution orders (50-first pass, then 250-first pass)
	// to separate a real batch-size effect from page-cache-warming
	// order artifacts, per the #7325 phase-1 precedent.
	for _, c := range []struct {
		label          string
		handler        *CodeHandler
		keys, tailKeys []string
	}{
		{"batch50-asc", handler50, memberIDs, tailSources},
		{"batch250-asc", handler250, memberIDs, tailSources},
		{"batch250-desc", handler250, descMembers, descTail},
		{"batch50-desc", handler50, descMembers, descTail},
	} {
		// Tail single-statement probe: at 250 keys this is exactly one
		// statement; at effective 50 it is five. 1 warm-up + 10 timed.
		tailRuns := make([]time.Duration, 0, 11)
		var tailEdges map[string][]codedivergence.OutlierCallerEdge
		for i := 0; i < 11; i++ {
			started := time.Now()
			edges, _, err := c.handler.readOutlierCalleeEdges(ctx, c.tailKeys, liveScaleRepo)
			if err != nil {
				t.Fatalf("tail probe @%s run %d: %v", c.label, i, err)
			}
			tailRuns = append(tailRuns, time.Since(started))
			tailEdges = edges
		}
		tailCold, tailP50, tailP95, tailMax := tailSummarize(tailRuns)
		tailHash := densityCanonicalEdgeHash(tailEdges)
		rows := 0
		for _, list := range tailEdges {
			rows += len(list)
		}
		t.Logf("%s tail probe (250 high-degree keys): rows=%d cold=%s warm-p50=%s warm-p95=%s max=%s edge-hash=%s",
			c.label, rows, tailCold, tailP50, tailP95, tailMax, tailHash)

		// Whole-cohort fan-out: 1 warm-up + 1 timed.
		if _, _, err := c.handler.readOutlierCalleeEdges(ctx, c.keys, liveScaleRepo); err != nil {
			t.Fatalf("readOutlierCalleeEdges warm-up @%s: %v", c.label, err)
		}
		fanOutStart := time.Now()
		edges, _, err := c.handler.readOutlierCalleeEdges(ctx, c.keys, liveScaleRepo)
		if err != nil {
			t.Fatalf("readOutlierCalleeEdges @%s: %v", c.label, err)
		}
		fanOutElapsed := time.Since(fanOutStart)
		edgeHash := densityCanonicalEdgeHash(edges)

		// Full sweep: 1 cold + 10 warm.
		const fullRuns = 11
		durations := make([]time.Duration, 0, fullRuns)
		var findings []codedivergence.Finding
		for i := 0; i < fullRuns; i++ {
			started := time.Now()
			f, _, err := c.handler.assembleOutlierTrack(ctx, liveScaleRepo, false)
			if err != nil {
				t.Fatalf("assembleOutlierTrack @%s run %d: %v", c.label, i, err)
			}
			durations = append(durations, time.Since(started))
			findings = f
		}
		fullCold, fullP50, fullP95, _ := tailSummarize(durations)
		findingHash := densityCanonicalFindingHash(findings)
		t.Logf("%s: fan-out=%s full-cold=%s full-warm-p50=%s full-warm-p95=%s findings=%d edge-hash=%s finding-hash=%s",
			c.label, fanOutElapsed, fullCold, fullP50, fullP95, len(findings), edgeHash, findingHash)

		cells = append(cells, cell{
			label:   c.label,
			tailP95: tailP95, tailMax: tailMax,
			tailHash: tailHash, edgeHash: edgeHash, findingHash: findingHash,
			findingCount: len(findings),
		})
	}

	if cells[0].findingCount == 0 {
		t.Errorf("%s produced zero findings; the signal fixture should produce at least one", cells[0].label)
	}
	for i := 1; i < len(cells); i++ {
		if cells[i].tailHash != cells[0].tailHash {
			t.Errorf("tail edge multiset differs: %s hash=%s != %s hash=%s",
				cells[i].label, cells[i].tailHash, cells[0].label, cells[0].tailHash)
		}
		if cells[i].edgeHash != cells[0].edgeHash {
			t.Errorf("callee-edge multiset differs between cells: %s hash=%s != %s hash=%s",
				cells[i].label, cells[i].edgeHash, cells[0].label, cells[0].edgeHash)
		}
		if cells[i].findingHash != cells[0].findingHash {
			t.Errorf("finding set differs between cells: %s hash=%s (%d findings) != %s hash=%s (%d findings)",
				cells[i].label, cells[i].findingHash, cells[i].findingCount,
				cells[0].label, cells[0].findingHash, cells[0].findingCount)
		}
	}
	for _, cl := range cells {
		budget := "UNDER"
		if cl.tailP95 >= time.Second || cl.tailMax >= time.Second {
			budget = "OVER"
		}
		t.Logf("BUDGET %s tail single-statement warm-p95=%s max=%s vs 1s: %s",
			cl.label, cl.tailP95, cl.tailMax, budget)
	}
	t.Logf("SUMMARY nonce=%q members=%d tail-rows-anchor-degree=%d rows-equal=%v findings-equal=%v (%d findings)",
		nonce, len(memberIDs), tailDegreeForChunkRank(0),
		cells[0].tailHash == cells[1].tailHash && cells[1].tailHash == cells[2].tailHash && cells[2].tailHash == cells[3].tailHash,
		cells[0].findingHash == cells[1].findingHash && cells[1].findingHash == cells[2].findingHash && cells[2].findingHash == cells[3].findingHash,
		cells[0].findingCount)
}
