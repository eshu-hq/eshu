// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for #6690: the value-flow cloud sink loader finds the cloud sinks
// the seeded graph implies, through the real loader, on the pinned NornicDB
// image.
//
// Run against an isolated container on the pinned image:
//
//	docker run -d --name eshu-answer-truth -e NORNICDB_NO_AUTH=true \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -p 127.0.0.1:27687:7687 \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468@sha256:eb69530fa2951d74d89ed9df947aeea10beb0c5e2f2ede4c0080e09fe78aa555
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27687 go test ./internal/reducer/code/value \
//	  -tags live_nornicdb_answer_truth -run TestLiveCloudSinkLoader -count=1 -v
//
// ESHU_NEO4J_DATABASE selects the database (default "nornic"; use "neo4j" for a
// Neo4j positive control).
package value

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/exposure"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// cloudSinkLiveSeed uses literal property values only: on the pinned v1.3.3
// image an expression inside a CREATE property map can be stored as mangled
// literal text.
//
// Ground truth, by construction:
//   - fn-one invokes s3:GetObject and runs in exactly one workload (through two
//     duplicate RUNS_IN edges), whose instance uses a principal allowed to
//     perform s3:GetObject on a bucket: one sink.
//   - fn-two invokes the same action but runs in two workloads: excluded.
//   - fn-miss runs in the same single workload but invokes an action the
//     principal is not allowed: no sink.
//   - fn-noinst runs in a workload with no instance: no sink.
var cloudSinkLiveSeed = []string{
	`CREATE (:Function {uid: 'answer-truth-valueflow:vf-one', name: 'One'})`,
	`CREATE (:Function {uid: 'answer-truth-valueflow:vf-two', name: 'Two'})`,
	`CREATE (:Function {uid: 'answer-truth-valueflow:vf-miss', name: 'Miss'})`,
	`CREATE (:Function {uid: 'answer-truth-valueflow:vf-noinst', name: 'NoInstance'})`,
	`CREATE (:CloudAction {action: 'answer-truth-valueflow:s3:GetObject'})`,
	`CREATE (:CloudAction {action: 'answer-truth-valueflow:sqs:SendMessage'})`,
	`CREATE (:Workload {id: 'answer-truth-valueflow:vw-1'})`,
	`CREATE (:Workload {id: 'answer-truth-valueflow:vw-2'})`,
	`CREATE (:Workload {id: 'answer-truth-valueflow:vw-empty'})`,
	`CREATE (:WorkloadInstance {id: 'answer-truth-valueflow:vi-1'})`,
	`CREATE (:WorkloadInstance {id: 'answer-truth-valueflow:vi-2'})`,
	`CREATE (:CloudResource {id: 'answer-truth-valueflow:vp-role'})`,
	`CREATE (:CloudResource {id: 'answer-truth-valueflow:vs-bucket', is_internet: false})`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-one'}) MATCH (a:CloudAction {action: 'answer-truth-valueflow:s3:GetObject'}) CREATE (f)-[:INVOKES_CLOUD_ACTION]->(a)`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-two'}) MATCH (a:CloudAction {action: 'answer-truth-valueflow:s3:GetObject'}) CREATE (f)-[:INVOKES_CLOUD_ACTION]->(a)`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-noinst'}) MATCH (a:CloudAction {action: 'answer-truth-valueflow:s3:GetObject'}) CREATE (f)-[:INVOKES_CLOUD_ACTION]->(a)`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-miss'}) MATCH (a:CloudAction {action: 'answer-truth-valueflow:sqs:SendMessage'}) CREATE (f)-[:INVOKES_CLOUD_ACTION]->(a)`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-one'}) MATCH (w:Workload {id: 'answer-truth-valueflow:vw-1'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-one'}) MATCH (w:Workload {id: 'answer-truth-valueflow:vw-1'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-two'}) MATCH (w:Workload {id: 'answer-truth-valueflow:vw-1'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-two'}) MATCH (w:Workload {id: 'answer-truth-valueflow:vw-2'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-miss'}) MATCH (w:Workload {id: 'answer-truth-valueflow:vw-1'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-valueflow:vf-noinst'}) MATCH (w:Workload {id: 'answer-truth-valueflow:vw-empty'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-valueflow:vi-1'}) MATCH (w:Workload {id: 'answer-truth-valueflow:vw-1'}) CREATE (i)-[:INSTANCE_OF]->(w)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-valueflow:vi-2'}) MATCH (w:Workload {id: 'answer-truth-valueflow:vw-2'}) CREATE (i)-[:INSTANCE_OF]->(w)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-valueflow:vi-1'}) MATCH (p:CloudResource {id: 'answer-truth-valueflow:vp-role'}) CREATE (i)-[:USES]->(p)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-valueflow:vi-2'}) MATCH (p:CloudResource {id: 'answer-truth-valueflow:vp-role'}) CREATE (i)-[:USES]->(p)`,
	`MATCH (p:CloudResource {id: 'answer-truth-valueflow:vp-role'}) MATCH (s:CloudResource {id: 'answer-truth-valueflow:vs-bucket'}) CREATE (p)-[:CAN_PERFORM {actions: ['answer-truth-valueflow:s3:GetObject']}]->(s)`,
}

const cloudSinkLiveCleanup = `MATCH (n)
WHERE n.uid STARTS WITH 'answer-truth-valueflow:' OR n.id STARTS WITH 'answer-truth-valueflow:' OR n.action STARTS WITH 'answer-truth-valueflow:'
DETACH DELETE n`

func TestLiveCloudSinkLoader(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "nornic"
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

	fnOne := summary.NewFunctionID("repo-a", "pkg", "", "one")
	graphIDs := map[summary.FunctionID]string{
		fnOne: "answer-truth-valueflow:vf-one",
		summary.NewFunctionID("repo-a", "pkg", "", "two"):    "answer-truth-valueflow:vf-two",
		summary.NewFunctionID("repo-a", "pkg", "", "miss"):   "answer-truth-valueflow:vf-miss",
		summary.NewFunctionID("repo-a", "pkg", "", "noinst"): "answer-truth-valueflow:vf-noinst",
	}
	targets, err := GraphCloudSinkTargetLoader{Graph: graph}.LoadCloudSinkTargets(ctx, graphIDs)
	if err != nil {
		t.Fatalf("LoadCloudSinkTargets: %v", err)
	}
	t.Logf("targets: %+v", targets)
	want := []CloudSinkTarget{{
		FunctionID: fnOne,
		Kind:       string(exposure.SinkIAMPrivilegedAction),
		Label:      "IAM effective privileged action",
	}}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %+v, want %+v", targets, want)
	}

	// The two statements are separate autocommit reads. Change RUNS_IN in the
	// window between them: after the first read has classified fn-one as
	// single-workload, fn-one also starts running in vw-2 (which has its own
	// instance and the same principal). The sink read must see that and drop
	// fn-one rather than trust the first read.
	racing := &cloudSinkRacingGraph{
		inner: graph,
		betweenReads: func() {
			graph.write(ctx, t, `MATCH (f:Function {uid: 'answer-truth-valueflow:vf-one'}) MATCH (w:Workload {id: 'answer-truth-valueflow:vw-2'}) CREATE (f)-[:RUNS_IN]->(w)`)
		},
	}
	raced, err := GraphCloudSinkTargetLoader{Graph: racing}.LoadCloudSinkTargets(ctx, graphIDs)
	if err != nil {
		t.Fatalf("LoadCloudSinkTargets with RUNS_IN changed between reads: %v", err)
	}
	t.Logf("targets after RUNS_IN changed between reads: %+v", raced)
	if !racing.fired {
		t.Fatal("the RUNS_IN change never ran between the two reads")
	}
	if len(raced) != 0 {
		t.Fatalf("targets = %+v, want none: fn-one runs in two workloads by the time the sink read runs", raced)
	}
}

// pre6761CloudSinkTargetsCypher is the single statement the loader ran before
// #6761: it aggregated workloads per (function, action) pair in-query
// (collect(DISTINCT workload) / size(workloads) = 1 / workloads[0]) and matched
// on after a WITH. On NornicDB v1.3.3 it returns zero rows with no error
// (#6690, orneryd/NornicDB#400), silently emptying every value-flow cloud sink
// projection. It is pinned here so the B-7 sl-value-flow-cloud-sink row (#6785)
// keeps a proven RED case: this shape can never satisfy that row.
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

// cloudSinkRacingGraph runs betweenReads once, right after the first cloud
// sink statement returns and before the second one starts.
type cloudSinkRacingGraph struct {
	inner        cloudSinkLiveGraph
	betweenReads func()
	fired        bool
}

func (g *cloudSinkRacingGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	rows, err := g.inner.Run(ctx, cypher, params)
	if err == nil && cypher == CloudSinkWorkloadRowsCypher && !g.fired {
		g.fired = true
		g.betweenReads()
	}
	return rows, err
}

type cloudSinkLiveGraph struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (g cloudSinkLiveGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: g.database})
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
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (g cloudSinkLiveGraph) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: g.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume %q: %v", cypher, err)
	}
}
