// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build graph_entity_inventory_slo_live

package query

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	graphEntityInventorySLONodeCount  = 91000
	graphEntityInventorySLOFacetCount = 700
	graphEntityInventorySLOBatchSize  = 1000
	graphEntityInventorySLOLimit      = 3 * time.Second
	graphEntityInventorySLOProfileEnv = "ESHU_GRAPH_ENTITY_INVENTORY_SLO_PROFILE"
)

func TestGraphEntityInventoryInteractiveSLO(t *testing.T) {
	if os.Getenv("ESHU_GRAPH_ENTITY_INVENTORY_SLO_LIVE") != "1" {
		t.Skip("set ESHU_GRAPH_ENTITY_INVENTORY_SLO_LIVE=1 to run the live SLO proof")
	}
	if os.Getenv("ESHU_GRAPH_ENTITY_INVENTORY_SLO_ISOLATED") != "1" {
		t.Fatal("ESHU_GRAPH_ENTITY_INVENTORY_SLO_ISOLATED=1 is required because this test replaces graph data")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	driver, database := openGraphEntityInventorySLOGraph(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedGraphEntityInventorySLOGraph(ctx, t, driver, database)
	reader := NewNeo4jReader(driver, database)

	oldDurations := make([]time.Duration, 4)
	candidateDurations := make([]time.Duration, 4)
	for run := range oldDurations {
		oldStart := time.Now()
		oldKinds, oldTotal := oldGraphEntityInventoryCounts(ctx, t, reader)
		oldDurations[run] = time.Since(oldStart)

		candidateStart := time.Now()
		rows, err := reader.Run(ctx, graphEntityKindCountsCypher(graphEntityKinds), nil)
		candidateDurations[run] = time.Since(candidateStart)
		if err != nil {
			t.Fatalf("candidate count run %d: %v", run+1, err)
		}
		candidateKinds, candidateTotal, err := decodeGraphEntityKindCounts(rows)
		if err != nil {
			t.Fatalf("decode candidate count run %d: %v", run+1, err)
		}
		if !reflect.DeepEqual(candidateKinds, oldKinds) || candidateTotal != oldTotal {
			t.Fatalf("candidate run %d differs from old counts: old=%#v/%d candidate=%#v/%d",
				run+1, oldKinds, oldTotal, candidateKinds, candidateTotal)
		}
		if candidateDurations[run] > graphEntityInventorySLOLimit {
			t.Fatalf("candidate run %d duration = %s, exceeds interactive SLO %s",
				run+1, candidateDurations[run], graphEntityInventorySLOLimit)
		}
	}
	dbHitsEvidence := "db_hits=unavailable"
	if os.Getenv(graphEntityInventorySLOProfileEnv) == "1" {
		oldDBHits, oldDBHitsAvailable := graphEntityInventorySLOOldDBHits(ctx, t, driver, database)
		candidateDBHits, candidateDBHitsAvailable := graphEntityInventorySLOProfileDBHits(
			ctx, t, driver, database, graphEntityKindCountsCypher(graphEntityKinds),
		)
		if oldDBHitsAvailable && candidateDBHitsAvailable {
			dbHitsEvidence = fmt.Sprintf("old_db_hits=%d candidate_db_hits=%d", oldDBHits, candidateDBHits)
		}
	}
	zeroKind := graphEntityKinds[len(graphEntityKinds)-1]
	deleteGraphEntityInventorySLOLabel(ctx, t, driver, database, zeroKind.label)
	zeroOldKinds, zeroOldTotal := oldGraphEntityInventoryCounts(ctx, t, reader)
	zeroRows, err := reader.Run(ctx, graphEntityKindCountsCypher(graphEntityKinds), nil)
	if err != nil {
		t.Fatalf("zero-label candidate count: %v", err)
	}
	zeroCandidateKinds, zeroCandidateTotal, err := decodeGraphEntityKindCounts(zeroRows)
	if err != nil {
		t.Fatalf("decode zero-label candidate count: %v", err)
	}
	if !reflect.DeepEqual(zeroCandidateKinds, zeroOldKinds) || zeroCandidateTotal != zeroOldTotal {
		t.Fatalf("zero-label candidate differs from old counts: old=%#v/%d candidate=%#v/%d",
			zeroOldKinds, zeroOldTotal, zeroCandidateKinds, zeroCandidateTotal)
	}

	warmOld := append([]time.Duration(nil), oldDurations[1:]...)
	warmCandidate := append([]time.Duration(nil), candidateDurations[1:]...)
	sort.Slice(warmOld, func(i, j int) bool { return warmOld[i] < warmOld[j] })
	sort.Slice(warmCandidate, func(i, j int) bool { return warmCandidate[i] < warmCandidate[j] })
	t.Logf(
		"nodes=%d facet_nodes=%d old_round_trips=8 candidate_round_trips=1 old_first=%s old_warm_median=%s candidate_first=%s candidate_warm_median=%s limit=%s exact_counts=true zero_label_exact=true %s",
		graphEntityInventorySLONodeCount,
		len(graphEntityKinds)*graphEntityInventorySLOFacetCount,
		oldDurations[0],
		warmOld[1],
		candidateDurations[0],
		warmCandidate[1],
		graphEntityInventorySLOLimit,
		dbHitsEvidence,
	)
}

func graphEntityInventorySLOOldDBHits(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
) (int64, bool) {
	t.Helper()
	total := int64(0)
	for _, kind := range graphEntityKinds {
		hits, available := graphEntityInventorySLOProfileDBHits(
			ctx, t, driver, database, "MATCH (n:"+kind.label+") RETURN count(n) AS c",
		)
		if !available {
			return 0, false
		}
		total += hits
	}
	return total, true
}

func graphEntityInventorySLOProfileDBHits(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
	cypher string,
) (int64, bool) {
	t.Helper()
	profileCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	session := driver.NewSession(profileCtx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(profileCtx) }()
	result, err := session.Run(profileCtx, "PROFILE "+cypher, nil)
	if err != nil {
		t.Logf("PROFILE db-hit evidence unavailable: %v", err)
		return 0, false
	}
	summary, err := result.Consume(profileCtx)
	if err != nil {
		t.Logf("consume PROFILE db-hit evidence: %v", err)
		return 0, false
	}
	profile := summary.Profile()
	if profile == nil {
		return 0, false
	}
	return graphEntityInventorySLOPlanDBHits(profile), true
}

func graphEntityInventorySLOPlanDBHits(plan neo4jdriver.ProfiledPlan) int64 {
	total := plan.DbHits()
	for _, child := range plan.Children() {
		total += graphEntityInventorySLOPlanDBHits(child)
	}
	return total
}

func deleteGraphEntityInventorySLOLabel(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
	label string,
) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(context.Background()) }()
	runGraphEntityInventorySLOWrite(ctx, t, session, "MATCH (n:"+label+") DETACH DELETE n", nil)
}

func openGraphEntityInventorySLOGraph(
	ctx context.Context,
	t *testing.T,
) (neo4jdriver.DriverWithContext, string) {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	auth := neo4jdriver.NoAuth()
	if username := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USERNAME")); username != "" {
		auth = neo4jdriver.BasicAuth(username, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("verify graph connectivity: %v", err)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	return driver, database
}

func seedGraphEntityInventorySLOGraph(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	database string,
) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(context.Background()) }()
	runGraphEntityInventorySLOWrite(ctx, t, session, "MATCH (node) DETACH DELETE node", nil)
	for _, kind := range graphEntityKinds {
		seedGraphEntityInventorySLOLabel(ctx, t, session, kind.label, graphEntityInventorySLOFacetCount)
	}
	fillerCount := graphEntityInventorySLONodeCount - len(graphEntityKinds)*graphEntityInventorySLOFacetCount
	seedGraphEntityInventorySLOLabel(ctx, t, session, "GraphEntityInventorySLOFiller", fillerCount)
}

func seedGraphEntityInventorySLOLabel(
	ctx context.Context,
	t *testing.T,
	session neo4jdriver.SessionWithContext,
	label string,
	count int,
) {
	t.Helper()
	for start := 0; start < count; start += graphEntityInventorySLOBatchSize {
		end := min(start+graphEntityInventorySLOBatchSize, count)
		rows := make([]map[string]any, 0, end-start)
		for index := start; index < end; index++ {
			rows = append(rows, map[string]any{"id": fmt.Sprintf("%s:%d", label, index)})
		}
		runGraphEntityInventorySLOWrite(
			ctx,
			t,
			session,
			"UNWIND $rows AS row CREATE (n:"+label+" {id: row.id})",
			map[string]any{"rows": rows},
		)
	}
}

func runGraphEntityInventorySLOWrite(
	ctx context.Context,
	t *testing.T,
	session neo4jdriver.SessionWithContext,
	cypher string,
	params map[string]any,
) {
	t.Helper()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run SLO graph write: %v", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume SLO graph write: %v", err)
	}
}

func oldGraphEntityInventoryCounts(
	ctx context.Context,
	t *testing.T,
	reader GraphQuery,
) ([]map[string]any, int) {
	t.Helper()
	kinds := make([]map[string]any, 0, len(graphEntityKinds))
	total := 0
	for _, kind := range graphEntityKinds {
		row, err := reader.RunSingle(ctx, "MATCH (n:"+kind.label+") RETURN count(n) AS c", nil)
		if err != nil {
			t.Fatalf("old %s count: %v", kind.key, err)
		}
		count := IntVal(row, "c")
		total += count
		kinds = append(kinds, map[string]any{"kind": kind.key, "label": kind.label, "count": count})
	}
	return kinds, total
}
