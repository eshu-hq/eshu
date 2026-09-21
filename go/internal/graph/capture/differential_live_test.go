// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value"
)

// Live proof for #6782 slice 3: the capture-to-diff chain fires on a real
// backend divergence and stays quiet on agreement, through the real
// production statements, not copies.
//
// Run against two isolated containers (NornicDB pinned plus a Neo4j
// positive control):
//
//	docker run -d --name eshu-6786-verify-nornic -e NORNICDB_NO_AUTH=true \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -p 127.0.0.1:27920:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f
//	docker run -d --name eshu-6786-verify-neo4j -p 127.0.0.1:27930:7687 neo4j:<pin>
//	cd go && ESHU_DIFFERENTIAL_CAPTURE=1 ESHU_DIFFERENTIAL_LIVE=1 \
//	  ESHU_DIFFERENTIAL_NORNICDB_URI=bolt://127.0.0.1:27920 \
//	  ESHU_DIFFERENTIAL_NEO4J_URI=bolt://127.0.0.1:27930 \
//	  go test ./internal/graph/capture -run TestLiveDifferentialCloudSink -count=1 -v
//
// Without ESHU_DIFFERENTIAL_LIVE=1 the test skips: it needs two live
// backends at once, which default unit runs never have.
// ESHU_DIFFERENTIAL_CAPTURE=1 must also be set: the capture decorators
// stay a silent passthrough without the opt-in flag, and this test proves
// that full production path, not a recorder-only shortcut.
const (
	differentialLiveEnv   = "ESHU_DIFFERENTIAL_LIVE"
	differentialNornicEnv = "ESHU_DIFFERENTIAL_NORNICDB_URI"
	differentialNeo4jEnv  = "ESHU_DIFFERENTIAL_NEO4J_URI"
)

// differentialLiveSeed is cloud_sink_loader_live_test.go's cloudSinkLiveSeed
// with the fixture prefix swapped to answer-truth-differential:, so this
// test's fixtures never share nodes or edges with a parallel run of the
// value-flow live tests against the same containers. The ground truth is
// unchanged: fn-one invokes s3:GetObject and runs in exactly one workload
// (one sink); fn-two runs in two workloads (excluded); fn-miss invokes an
// unauthorized action (no sink); fn-noinst runs in a workload with no
// instance (no sink).
var differentialLiveSeed = []string{
	`CREATE (:Function {uid: 'answer-truth-differential:vf-one', name: 'One'})`,
	`CREATE (:Function {uid: 'answer-truth-differential:vf-two', name: 'Two'})`,
	`CREATE (:Function {uid: 'answer-truth-differential:vf-miss', name: 'Miss'})`,
	`CREATE (:Function {uid: 'answer-truth-differential:vf-noinst', name: 'NoInstance'})`,
	`CREATE (:CloudAction {action: 'answer-truth-differential:s3:GetObject'})`,
	`CREATE (:CloudAction {action: 'answer-truth-differential:sqs:SendMessage'})`,
	`CREATE (:Workload {id: 'answer-truth-differential:vw-1'})`,
	`CREATE (:Workload {id: 'answer-truth-differential:vw-2'})`,
	`CREATE (:Workload {id: 'answer-truth-differential:vw-empty'})`,
	`CREATE (:WorkloadInstance {id: 'answer-truth-differential:vi-1'})`,
	`CREATE (:WorkloadInstance {id: 'answer-truth-differential:vi-2'})`,
	`CREATE (:CloudResource {id: 'answer-truth-differential:vp-role'})`,
	`CREATE (:CloudResource {id: 'answer-truth-differential:vs-bucket', is_internet: false})`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-one'}) MATCH (a:CloudAction {action: 'answer-truth-differential:s3:GetObject'}) CREATE (f)-[:INVOKES_CLOUD_ACTION]->(a)`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-two'}) MATCH (a:CloudAction {action: 'answer-truth-differential:s3:GetObject'}) CREATE (f)-[:INVOKES_CLOUD_ACTION]->(a)`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-noinst'}) MATCH (a:CloudAction {action: 'answer-truth-differential:s3:GetObject'}) CREATE (f)-[:INVOKES_CLOUD_ACTION]->(a)`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-miss'}) MATCH (a:CloudAction {action: 'answer-truth-differential:sqs:SendMessage'}) CREATE (f)-[:INVOKES_CLOUD_ACTION]->(a)`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-one'}) MATCH (w:Workload {id: 'answer-truth-differential:vw-1'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-one'}) MATCH (w:Workload {id: 'answer-truth-differential:vw-1'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-two'}) MATCH (w:Workload {id: 'answer-truth-differential:vw-1'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-two'}) MATCH (w:Workload {id: 'answer-truth-differential:vw-2'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-miss'}) MATCH (w:Workload {id: 'answer-truth-differential:vw-1'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (f:Function {uid: 'answer-truth-differential:vf-noinst'}) MATCH (w:Workload {id: 'answer-truth-differential:vw-empty'}) CREATE (f)-[:RUNS_IN]->(w)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-differential:vi-1'}) MATCH (w:Workload {id: 'answer-truth-differential:vw-1'}) CREATE (i)-[:INSTANCE_OF]->(w)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-differential:vi-2'}) MATCH (w:Workload {id: 'answer-truth-differential:vw-2'}) CREATE (i)-[:INSTANCE_OF]->(w)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-differential:vi-1'}) MATCH (p:CloudResource {id: 'answer-truth-differential:vp-role'}) CREATE (i)-[:USES]->(p)`,
	`MATCH (i:WorkloadInstance {id: 'answer-truth-differential:vi-2'}) MATCH (p:CloudResource {id: 'answer-truth-differential:vp-role'}) CREATE (i)-[:USES]->(p)`,
	`MATCH (p:CloudResource {id: 'answer-truth-differential:vp-role'}) MATCH (s:CloudResource {id: 'answer-truth-differential:vs-bucket'}) CREATE (p)-[:CAN_PERFORM {actions: ['answer-truth-differential:s3:GetObject']}]->(s)`,
}

const differentialLiveCleanup = `MATCH (n)
WHERE n.uid STARTS WITH 'answer-truth-differential:' OR n.id STARTS WITH 'answer-truth-differential:' OR n.action STARTS WITH 'answer-truth-differential:'
DETACH DELETE n`

// differentialPre6761Cypher is the single statement the cloud sink loader
// ran before #6761, copied from cloud_sink_loader_live_test.go's
// pre6761CloudSinkTargetsCypher with only the fixture prefix swapped (same
// swap as the seed). On NornicDB v1.3.3 it returns zero rows with no error
// (#6690, orneryd/NornicDB#400) where Neo4j answers: the seeded RED case
// for the capture-to-diff chain.
const differentialPre6761Cypher = `MATCH (fn:Function)-[:INVOKES_CLOUD_ACTION]->(action:CloudAction)
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

// differentialLiveGraph is a minimal Bolt read/write handle for the seeded
// fixture: writes run raw, reads run through the capture decorator.
type differentialLiveGraph struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (g differentialLiveGraph) write(ctx context.Context, t *testing.T, cypher string) {
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

func (g differentialLiveGraph) captured(backend string, recorder *backendconformance.DifferentialRecorder) backendconformance.GraphQuery {
	return backendconformance.WrapGraphQuery(differentialLiveReader{graph: g}, recorder, backend)
}

// differentialLiveReader adapts the Bolt handle to the capture seam.
type differentialLiveReader struct {
	graph differentialLiveGraph
}

func (r differentialLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.graph.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: r.graph.database})
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

func (r differentialLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func openDifferentialDriver(t *testing.T, uri string) neo4jdriver.DriverWithContext {
	t.Helper()
	auth := neo4jdriver.NoAuth()
	if user := os.Getenv("ESHU_NEO4J_USERNAME"); user != "" {
		auth = neo4jdriver.BasicAuth(user, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatalf("open driver %s: %v", uri, err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	return driver
}

// TestLiveDifferentialCloudSink proves the capture-to-diff chain against
// two live backends: the pre-#6761 statement diverges (RED — the gate
// would fail), and the current two-statement loader agrees (GREEN — the
// gate stays quiet).
func TestLiveDifferentialCloudSink(t *testing.T) {
	if os.Getenv(differentialLiveEnv) != "1" {
		t.Skipf("set %s=1 with both backend URIs to run the live differential proof", differentialLiveEnv)
	}
	// The decorators consult the real opt-in flag, so the test sets it: the
	// point is to prove the production capture path end to end.
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	nornicURI := strings.TrimSpace(os.Getenv(differentialNornicEnv))
	neoURI := strings.TrimSpace(os.Getenv(differentialNeo4jEnv))
	if nornicURI == "" || neoURI == "" {
		t.Fatalf("set %s and %s to run the live differential proof", differentialNornicEnv, differentialNeo4jEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	graphs := map[string]differentialLiveGraph{
		"nornicdb": {driver: openDifferentialDriver(t, nornicURI), database: "nornic"},
		"neo4j":    {driver: openDifferentialDriver(t, neoURI), database: "neo4j"},
	}
	uids := []string{
		"answer-truth-differential:vf-one",
		"answer-truth-differential:vf-two",
		"answer-truth-differential:vf-miss",
		"answer-truth-differential:vf-noinst",
	}
	seed := func(graph differentialLiveGraph) {
		graph.write(ctx, t, differentialLiveCleanup)
		for _, stmt := range differentialLiveSeed {
			graph.write(ctx, t, stmt)
		}
		// graph is a parameter, not a loop variable, so the deferred
		// cleanup keeps the backend it seeded.
		t.Cleanup(func() {
			graph.write(context.Background(), t, differentialLiveCleanup)
		})
	}
	for _, graph := range graphs {
		seed(graph)
	}

	// RED: the pre-#6761 statement through the capture decorator on both
	// backends must diverge exactly once, with NornicDB empty and Neo4j
	// answering.
	redRecorders := map[string]*backendconformance.DifferentialRecorder{
		"nornicdb": backendconformance.NewDifferentialRecorder(),
		"neo4j":    backendconformance.NewDifferentialRecorder(),
	}
	redCounts := map[string]int{}
	for backend, graph := range graphs {
		rows, err := graph.captured(backend, redRecorders[backend]).Run(ctx, differentialPre6761Cypher, map[string]any{"function_uids": uids})
		if err != nil {
			t.Fatalf("pre-6761 statement on %s: %v", backend, err)
		}
		redCounts[backend] = len(rows)
	}
	if redCounts["nornicdb"] != 0 {
		t.Fatalf("pre-6761 statement on nornicdb returned %d rows, want 0 (the #6690 empty-result defect)", redCounts["nornicdb"])
	}
	if redCounts["neo4j"] == 0 {
		t.Fatal("pre-6761 statement on neo4j returned 0 rows, want the positive control to answer")
	}
	redDiffs := backendconformance.CompareRecordings(redRecorders["nornicdb"].Records(), redRecorders["neo4j"].Records())
	if len(redDiffs) != 1 {
		t.Fatalf("pre-6761 comparison diffs = %d, want exactly 1: %+v", len(redDiffs), redDiffs)
	}
	if !strings.Contains(redDiffs[0].Fingerprint.Statement, "INVOKES_CLOUD_ACTION") {
		t.Fatalf("pre-6761 diff names %q, want the cloud sink statement", redDiffs[0].Fingerprint.Statement)
	}

	// GREEN: the current production loader through the same decorator on
	// both backends must agree.
	graphIDs := map[summary.FunctionID]string{
		summary.NewFunctionID("repo-a", "pkg", "", "one"):    "answer-truth-differential:vf-one",
		summary.NewFunctionID("repo-a", "pkg", "", "two"):    "answer-truth-differential:vf-two",
		summary.NewFunctionID("repo-a", "pkg", "", "miss"):   "answer-truth-differential:vf-miss",
		summary.NewFunctionID("repo-a", "pkg", "", "noinst"): "answer-truth-differential:vf-noinst",
	}
	greenRecorders := map[string]*backendconformance.DifferentialRecorder{
		"nornicdb": backendconformance.NewDifferentialRecorder(),
		"neo4j":    backendconformance.NewDifferentialRecorder(),
	}
	for backend, graph := range graphs {
		targets, err := value.GraphCloudSinkTargetLoader{Graph: graph.captured(backend, greenRecorders[backend])}.LoadCloudSinkTargets(ctx, graphIDs)
		if err != nil {
			t.Fatalf("current loader on %s: %v", backend, err)
		}
		if len(targets) != 1 {
			t.Fatalf("current loader on %s targets = %+v, want the single fn-one sink", backend, targets)
		}
	}
	if diffs := backendconformance.CompareRecordings(greenRecorders["nornicdb"].Records(), greenRecorders["neo4j"].Records()); len(diffs) != 0 {
		t.Fatalf("current loader comparison diffs = %+v, want clean", diffs)
	}
}
