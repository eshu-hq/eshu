// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Stale RED case for the B-7 sl-value-flow-cloud-sink row (#6785),
// split out of cloud_sink_loader_live_test.go so the passing loader tests
// stay blocking-CI while this shape is re-targeted.
//
// Status on the fix-500 pin (e022384c, #6915): STALE. Proven on a fresh
// database 2026-09-23: the pre-#6761 statement returns the CORRECT 1 row
// (vf-one's sink) instead of the #6690 empty result, matching Neo4j. The
// defect either no longer reproduces on upstream main 6ac958a9 or never
// reproduced with this seed. Re-target under #6787 (upstream tracking):
// find a still-broken shape on the current pin, or drop this file once
// the B-7 row carries its own sensitivity proof. Ledger class scheduled.
package value

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// pre6761CloudSinkTargetsCypher is the single statement the loader ran before
// #6761: it aggregated workloads per (function, action) pair in-query
// (collect(DISTINCT workload) / size(workloads) = 1 / workloads[0]) and matched
// on after a WITH. On NornicDB v1.3.3 it returns zero rows with no error
// (#6690, orneryd/NornicDB#400), silently emptying every value-flow cloud sink
// projection. It is pinned here so the B-7 sl-value-flow-cloud-sink row (#6785)
// keeps a proven RED case: this shape can never satisfy that row.
//
// STALE on the fix-500 pin: see the file header.
const pre6761CloudSinkTargetsCypher = `MATCH (fn:Function)-[:INVOKES_CLOUD_ACTION]->(action:CloudAction)
WHERE fn.uid IN $function_uids
MATCH (fn)-[:RUNS_IN]->(workload:Workload)
WITH fn, action, collect(DISTINCT workload) AS workloads
WHERE size(workloads) = 1
WITH fn, action, workloads[0] AS workload
MATCH (workload)<-[:INSTANCE_OF]-(instance:WorkloadInstance)-[:USES]->(principal:CloudResource)
MATCH (principal)-[sinkRel:CAN_PERFORM]->(sinkNode:CloudResource)
WHERE action.action IN sinkRel.actions
RETURN fn.uid AS function_uid,
       type(sinkRel) AS sink_rel,
       labels(sinkNode) AS sink_labels,
       sinkNode.is_internet AS sink_is_internet
ORDER BY function_uid, sink_rel`

// TestLivePre6761CloudSinkStatementReturnsNoRows pins the #6690 defect the
// B-7 sl-value-flow-cloud-sink row (#6785) guards: on the same seeded graph
// where the current two-statement loader finds fn-one's sink, the pre-#6761
// single statement returns zero rows on NornicDB v1.3.3 with no error. The
// contrast in one run proves sensitivity: the seed is sufficient (new shape
// finds the sink) and the old shape is empty (it cannot satisfy the gate row).
//
// STALE on the fix-500 pin (fails: returns 1 row). See the file header.
func TestLivePre6761CloudSinkStatementReturnsNoRows(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "nornic"
	}
	if database != "nornic" {
		t.Skip("the pre-#6761 empty-result defect is NornicDB-specific; Neo4j answers the old statement")
	}
	auth := neo4jdriver.NoAuth()
	if user := os.Getenv("ESHU_NEO4J_USERNAME"); user != "" {
		auth = neo4jdriver.BasicAuth(user, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	graph := cloudSinkLiveGraph{driver: driver, database: database}
	graph.write(ctx, t, cloudSinkLiveCleanup)
	for _, stmt := range cloudSinkLiveSeed {
		graph.write(ctx, t, stmt)
	}
	defer graph.write(context.Background(), t, cloudSinkLiveCleanup)

	uids := []string{
		"answer-truth-valueflow:vf-one",
		"answer-truth-valueflow:vf-two",
		"answer-truth-valueflow:vf-miss",
		"answer-truth-valueflow:vf-noinst",
	}
	oldRows, err := graph.Run(ctx, pre6761CloudSinkTargetsCypher, map[string]any{"function_uids": uids})
	if err != nil {
		t.Fatalf("pre-6761 statement: %v", err)
	}
	if len(oldRows) != 0 {
		t.Fatalf("pre-6761 statement returned %d rows, want 0 (the #6690 empty-result defect): %+v", len(oldRows), oldRows)
	}
}
