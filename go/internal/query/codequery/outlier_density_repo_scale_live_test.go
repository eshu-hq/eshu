// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// #7325 realistic-density repo-scale live proof. The #6929/#7297 seed
// (outlier_repo_scale_live_test.go) carries near-zero CALLS density among
// the swept repository's noise members: only the 11-node signal fixture has
// real outgoing CALLS edges. This file extends that seed with deterministic
// CALLS out-degree approximating the #6649 ops-qa reference distribution
// (mean 2.71, p50 1, p90 5, p99 21, docs/internal/evidence/
// 6649-calls-degree-floor.md), then proves the outlier CALLS-fanout's
// committed 250-key batch (outlierCalleeEdgeBatchSize,
// wrapper_bypass_track.go) returns byte-identical rows and findings to a
// 50-key batch at that density, through the real, unmodified production
// path (CodeHandler.assembleOutlierTrack / readOutlierCalleeEdges) at both
// sizes -- no production constant is mutated. The 50-key run wraps the real
// graph reader in subBatchSplittingReader, which further splits each
// already-chunked member_ids call into <=50-key sub-calls at the transport
// level and unions the rows; since row union is chunk-size-invariant (the
// same MATCH/UNWIND statement, just partitioned differently), this is
// exactly what a native 50-key chunker would have produced.
//
// Run against the container the #7325 evidence doc names:
//
//	docker run -d --name eshu-7325-neo4j -p 17325:7687 \
//	  -e NEO4J_AUTH=neo4j/eshu-7325-pass \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	ESHU_OUTLIER_NEO4J_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17325 \
//	  ESHU_NEO4J_USER=neo4j ESHU_NEO4J_PASSWORD=eshu-7325-pass \
//	  go test ./internal/query/codequery -run TestLiveOutlierDensityRepoScaleNeo4j -v -count=1 -timeout 20m
//	docker rm -f eshu-7325-neo4j
package codequery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// densityNoiseFunctionUIDs enumerates every noise Function uid seeded by
// liveScaleSeedNoiseFunctions, in the same deterministic order that
// function used to create them.
func densityNoiseFunctionUIDs() []string {
	uids := make([]string, 0, liveScaleNoiseFiles*liveScaleFuncsPerFile)
	for _, f := range liveScaleNoiseFileRows() {
		for j := 0; j < liveScaleFuncsPerFile; j++ {
			uids = append(uids, fmt.Sprintf("%s:fn:%03d", f.UID, j))
		}
	}
	return uids
}

// densityDegreeForRank returns the CALLS out-degree assigned to the member
// at rank i of n, deterministically shaped to approximate the #6649 ops-qa
// reference out-degree distribution (mean 2.71, p50 1, p90 5, p99 21,
// p99.9 53, max 521): most members get 1-5 callees, a small tail gets 20+.
func densityDegreeForRank(i, n int) int {
	frac := float64(i) / float64(n)
	switch {
	case frac < 0.60:
		return 1
	case frac < 0.78:
		return 2
	case frac < 0.86:
		return 3
	case frac < 0.90:
		return 5
	case frac < 0.98:
		return 6 + i%10 // 6..15
	case frac < 0.99:
		return 21
	default:
		return 25 + i%40 // 25..64: "a few 20+"
	}
}

// densitySeedCallsEdges adds deterministic, realistic CALLS out-degree
// among the swept repository's noise members (idempotent: skipped if the
// swept repo already carries more than the signal fixture's handful of
// CALLS edges). Returns the number of edges created.
func densitySeedCallsEdges(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext) int {
	t.Helper()

	existing := liveScaleCount(ctx, t, session, "count existing density CALLS edges",
		`MATCH (:Function {repo_id: $repo_id})-[r:CALLS]->() RETURN count(r) AS c`,
		map[string]any{"repo_id": liveScaleRepo})
	if existing > 100 {
		t.Logf("density CALLS edges already seeded (%d), skipping reseed", existing)
		return existing
	}

	uids := densityNoiseFunctionUIDs()
	n := len(uids)
	type edgeRow struct{ From, To string }
	rows := make([]edgeRow, 0, n*3)
	for i, from := range uids {
		degree := densityDegreeForRank(i, n)
		for k := 0; k < degree; k++ {
			// Deterministic pseudo-spread target selection: two large
			// coprime-ish strides keep targets from correlating with file
			// locality without needing math/rand.
			targetIdx := (i*7919 + k*104729 + 1) % n
			if targetIdx == i {
				targetIdx = (targetIdx + 1) % n
			}
			rows = append(rows, edgeRow{From: from, To: uids[targetIdx]})
		}
	}
	t.Logf("seeding %d density CALLS edges over %d members (mean degree %.3f)", len(rows), n, float64(len(rows))/float64(n))

	cypher := `
		UNWIND $rows AS row
		MATCH (from:Function {uid: row.from})
		MATCH (to:Function {uid: row.to})
		CREATE (from)-[r:CALLS]->(to)
		SET r.resolution_method = "declared", r.confidence = 0.9
	`
	batch := make([]map[string]any, 0, liveScaleWriteBatch)
	flush := func(idx int) {
		if len(batch) == 0 {
			return
		}
		liveScaleRunWrite(ctx, t, session, fmt.Sprintf("seed density CALLS batch %d", idx), cypher,
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
	return len(rows)
}

// densityDegreeStats queries the actual seeded out-degree distribution
// directly from Neo4j (not the generator's theoretical shape), so the
// reported percentiles are measured, not assumed.
func densityDegreeStats(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext) {
	t.Helper()

	result, err := session.Run(ctx, `
		MATCH (f:Function {repo_id: $repo_id})
		OPTIONAL MATCH (f)-[r:CALLS]->()
		WITH f, count(r) AS deg
		RETURN deg
		ORDER BY deg
	`, map[string]any{"repo_id": liveScaleRepo})
	if err != nil {
		t.Fatalf("degree stats: %v", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatalf("degree stats collect: %v", err)
	}
	degrees := make([]int64, 0, len(records))
	var sum int64
	for _, rec := range records {
		v, _ := rec.Get("deg")
		d, _ := v.(int64)
		degrees = append(degrees, d)
		sum += d
	}
	if len(degrees) == 0 {
		t.Fatalf("degree stats: no Function rows for repo %s", liveScaleRepo)
	}
	pct := func(p float64) int64 {
		idx := int(p * float64(len(degrees)-1))
		return degrees[idx]
	}
	mean := float64(sum) / float64(len(degrees))
	t.Logf("measured out-degree over %d Functions in %s: mean=%.3f p50=%d p90=%d p99=%d max=%d",
		len(degrees), liveScaleRepo, mean, pct(0.50), pct(0.90), pct(0.99), degrees[len(degrees)-1])
}

// subBatchSplittingReader wraps a real GraphQuery and, for any Run call
// whose params carry a "member_ids" key ([]string), further splits that
// key list into <=subBatch-sized pieces and unions the resulting rows --
// simulating a smaller UNWIND chunk size at the transport level without
// touching the committed outlierCalleeEdgeBatchSize constant. Every other
// call (no member_ids key, or RunSingle) passes straight through.
type subBatchSplittingReader struct {
	inner    GraphQuery
	subBatch int
}

func (r subBatchSplittingReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	ids, ok := params["member_ids"].([]string)
	if !ok || len(ids) <= r.subBatch {
		return r.inner.Run(ctx, cypher, params)
	}
	rows := []map[string]any{}
	for start := 0; start < len(ids); start += r.subBatch {
		end := start + r.subBatch
		if end > len(ids) {
			end = len(ids)
		}
		subParams := make(map[string]any, len(params))
		for k, v := range params {
			subParams[k] = v
		}
		subParams["member_ids"] = ids[start:end]
		subRows, err := r.inner.Run(ctx, cypher, subParams)
		if err != nil {
			return nil, err
		}
		rows = append(rows, subRows...)
	}
	return rows, nil
}

func (r subBatchSplittingReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return r.inner.RunSingle(ctx, cypher, params)
}

// densityCanonicalEdgeHash hashes the member->callee edge multiset returned
// by readOutlierCalleeEdges into a stable, sorted, delimited digest so
// rows-equal across batch sizes is byte-exact, not an eyeball check.
func densityCanonicalEdgeHash(edges map[string][]codedivergence.OutlierCallerEdge) string {
	rows := make([]string, 0, len(edges))
	for member, list := range edges {
		for _, e := range list {
			rows = append(rows, fmt.Sprintf("%s|%s|%s|%s|%.6f", member, e.CalleeID, e.CalleeName, e.EdgeMethod, e.EdgeConfidence))
		}
	}
	sort.Strings(rows)
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:])
}

// densityCanonicalFindingHash hashes the JSON-marshaled finding set:
// assembleOutlierTrack emits findings in deterministic (sorted cohort,
// sorted member id) order, so this is a stable digest across batch sizes.
func densityCanonicalFindingHash(findings []codedivergence.Finding) string {
	body, err := json.Marshal(findings)
	if err != nil {
		return "marshal-error:" + err.Error()
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// TestLiveOutlierDensityRepoScaleNeo4j is the #7325 realistic-density
// repo-scale proof: seed the #6929/#7297 shape plus deterministic CALLS
// out-degree among the swept repo's members, then run the real production
// path (readOutlierCalleeEdges, assembleOutlierTrack) at the committed
// 250-key batch and at an effective 50-key batch (via
// subBatchSplittingReader), asserting byte-identical rows and findings and
// logging both sizes' timings.
func TestLiveOutlierDensityRepoScaleNeo4j(t *testing.T) {
	liveScaleSkipUnset(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

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

	readSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = readSession.Close(ctx) }()

	densityDegreeStats(ctx, t, readSession)

	baseReader := newLiveNornicDBReader(driver, database)
	handler250 := liveScaleAuthedHandler(driver, database) // native production 250-key batch
	handler50 := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNeo4j,
		Neo4j:        subBatchSplittingReader{inner: baseReader, subBatch: 50},
		Content:      outlierFixtureStore{membersByID: liveScaleMembersByID()},
	}

	seeds, err := handler250.readOutlierCohortSeeds(ctx, liveScaleRepo, outlierCohortSources())
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
	t.Logf("cohort member count for fan-out comparison: %d", len(memberIDs))

	type cell struct {
		label                 string
		handler               *CodeHandler
		fanOutMs, fullP50Ms   float64
		edgeHash, findingHash string
		findingCount          int
	}
	cells := make([]cell, 0, 2)
	for _, c := range []struct {
		label   string
		handler *CodeHandler
	}{
		{"batch50(sub-split)", handler50},
		{"batch250(production)", handler250},
	} {
		// Warm-up, then a timed fan-out-only read.
		if _, _, err := c.handler.readOutlierCalleeEdges(ctx, memberIDs, liveScaleRepo); err != nil {
			t.Fatalf("readOutlierCalleeEdges warm-up @%s: %v", c.label, err)
		}
		fanOutStart := time.Now()
		edges, _, err := c.handler.readOutlierCalleeEdges(ctx, memberIDs, liveScaleRepo)
		if err != nil {
			t.Fatalf("readOutlierCalleeEdges @%s: %v", c.label, err)
		}
		fanOutElapsed := time.Since(fanOutStart)
		edgeHash := densityCanonicalEdgeHash(edges)

		const fullRuns = 6
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
		warm := append([]time.Duration(nil), durations[1:]...)
		sort.Slice(warm, func(i, j int) bool { return warm[i] < warm[j] })
		p50 := liveScalePercentile(warm, 0.5)
		findingHash := densityCanonicalFindingHash(findings)

		t.Logf("%s: fan-out=%s full-cold=%s warm-p50=%s findings=%d edge-hash=%s finding-hash=%s",
			c.label, fanOutElapsed, durations[0], p50, len(findings), edgeHash, findingHash)

		cells = append(cells, cell{
			label: c.label, handler: c.handler,
			fanOutMs: fanOutElapsed.Seconds() * 1000, fullP50Ms: p50.Seconds() * 1000,
			edgeHash: edgeHash, findingHash: findingHash, findingCount: len(findings),
		})
	}

	if cells[0].findingCount == 0 {
		t.Errorf("%s produced zero findings; the signal fixture should produce at least one", cells[0].label)
	}
	if cells[0].edgeHash != cells[1].edgeHash {
		t.Errorf("callee-edge multiset differs between batch sizes: %s hash=%s != %s hash=%s",
			cells[0].label, cells[0].edgeHash, cells[1].label, cells[1].edgeHash)
	}
	if cells[0].findingHash != cells[1].findingHash {
		t.Errorf("finding set differs between batch sizes: %s hash=%s (%d findings) != %s hash=%s (%d findings)",
			cells[0].label, cells[0].findingHash, cells[0].findingCount, cells[1].label, cells[1].findingHash, cells[1].findingCount)
	}
	t.Logf("SUMMARY batch50(sub-split) fan-out=%.1fms warm-p50=%.1fms; batch250(production) fan-out=%.1fms warm-p50=%.1fms; rows-equal=%v findings-equal=%v (%d findings)",
		cells[0].fanOutMs, cells[0].fullP50Ms, cells[1].fanOutMs, cells[1].fullP50Ms,
		cells[0].edgeHash == cells[1].edgeHash, cells[0].findingHash == cells[1].findingHash, cells[0].findingCount)
}
