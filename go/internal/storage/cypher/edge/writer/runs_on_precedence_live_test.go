// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type runsOnBoltExecutor struct {
	runner            *boltRetractTestRunner
	groupGate         *runsOnGroupGate
	forceInvalidFinal bool
}

type runsOnGroupGate struct {
	started        chan<- struct{}
	afterStatement int
	cypherContains string
	reached        chan<- struct{}
	release        <-chan struct{}
}

func (e runsOnBoltExecutor) Execute(ctx context.Context, statement sourcecypher.Statement) error {
	return e.runner.runCypherSingle(ctx, statement)
}

func (e runsOnBoltExecutor) ExecuteCypher(
	ctx context.Context,
	query string,
	parameters map[string]any,
) error {
	return e.Execute(ctx, sourcecypher.Statement{Cypher: query, Parameters: parameters})
}

func (e runsOnBoltExecutor) ExecuteCypherGroup(
	ctx context.Context,
	statements []reducer.CypherGroupStatement,
) error {
	group := make([]sourcecypher.Statement, 0, len(statements))
	for index, statement := range statements {
		query := statement.Cypher
		if e.forceInvalidFinal && index == len(statements)-1 {
			query = "THIS IS NOT VALID CYPHER"
		}
		group = append(group, sourcecypher.Statement{Cypher: query, Parameters: statement.Parameters})
	}
	return executeRunsOnGroup(ctx, e.runner, group, e.groupGate)
}

func (e runsOnBoltExecutor) ExecuteGroup(ctx context.Context, statements []sourcecypher.Statement) error {
	group := make([]sourcecypher.Statement, len(statements))
	copy(group, statements)
	if e.forceInvalidFinal && len(group) > 0 {
		group[len(group)-1].Cypher = "THIS IS NOT VALID CYPHER"
	}
	return executeRunsOnGroup(ctx, e.runner, group, e.groupGate)
}

func executeRunsOnGroup(
	ctx context.Context,
	runner *boltRetractTestRunner,
	statements []sourcecypher.Statement,
	gate *runsOnGroupGate,
) error {
	session := runner.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: runner.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		if gate != nil {
			notifyRunsOnGate(gate.started)
		}
		for index, statement := range statements {
			result, runErr := tx.Run(ctx, statement.Cypher, statement.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return nil, consumeErr
			}
			shouldGate := gate != nil && gate.afterStatement == index
			if gate != nil && gate.cypherContains != "" {
				shouldGate = strings.Contains(statement.Cypher, gate.cypherContains)
			}
			if shouldGate {
				notifyRunsOnGate(gate.reached)
				if gate.release != nil {
					select {
					case <-gate.release:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
			}
		}
		return nil, nil
	})
	return err
}

func notifyRunsOnGate(ch chan<- struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// TestBoltRunsOnWritersPreserveOneCoherentProvenanceTuple is the #6184
// writer-order regression for the shared WorkloadInstance-RUNS_ON-Platform
// identity. The workload materializer and cross-repo edge writer can run in
// either order, but once the cross-repo writer publishes its complete tuple a
// later workload pass must preserve all four properties together.
func TestBoltRunsOnWritersPreserveOneCoherentProvenanceTuple(t *testing.T) {
	runner := openRunsOnBoltTestRunner(t)
	ctx := context.Background()
	t.Cleanup(func() { runner.close(ctx) })
	executor := runsOnBoltExecutor{runner: runner}

	testCases := []struct {
		name       string
		order      string
		wantSource string
		wantTool   any
		wantReason string
		wantConf   float64
	}{
		{"workload_only", "w", reducer.EvidenceSourceWorkloads, nil, "Workload instance runs on inferred platform", 0.42},
		{"cross_repo_only", "x", reducer.CrossRepoEvidenceSource, "argocd", "Repository workload instance runs on inferred platform", 0.97},
		{"workload_then_cross_repo", "wx", reducer.CrossRepoEvidenceSource, "argocd", "Repository workload instance runs on inferred platform", 0.97},
		{"cross_repo_then_workload", "xw", reducer.CrossRepoEvidenceSource, "argocd", "Repository workload instance runs on inferred platform", 0.97},
		{"workload_cross_repo_workload", "wxw", reducer.CrossRepoEvidenceSource, "argocd", "Repository workload instance runs on inferred platform", 0.97},
		{"cross_repo_workload_cross_repo", "xwx", reducer.CrossRepoEvidenceSource, "argocd", "Repository workload instance runs on inferred platform", 0.97},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newRunsOnWriterFixture(t, runner, fmt.Sprintf("order-%d", index))

			materializer := reducer.NewWorkloadMaterializer(executor)
			workloadWrite := func() {
				t.Helper()
				_, err := materializer.Materialize(ctx, &reducer.ProjectionResult{
					RuntimePlatformRows: []reducer.RuntimePlatformRow{{
						Confidence:   0.42,
						InstanceID:   fixture.instanceID,
						PlatformID:   fixture.platformID,
						PlatformKind: "kubernetes",
						PlatformName: "pr6634-platform",
						RepoID:       fixture.repoID,
					}},
				})
				if err != nil {
					t.Fatalf("workload Materialize() error = %v", err)
				}
			}
			writer := NewEdgeWriter(executor, 1)
			crossRepoWrite := func() {
				t.Helper()
				_, err := writer.WriteEdges(ctx, reducer.DomainRepoDependency, []reducer.SharedProjectionIntentRow{{
					IntentID:     "pr6634-runs-on-intent-" + testCase.name,
					RepositoryID: fixture.repoID,
					Payload: map[string]any{
						"repo_id":           fixture.repoID,
						"platform_id":       fixture.platformID,
						"relationship_type": "RUNS_ON",
						"source_tool":       "argocd",
					},
				}}, reducer.CrossRepoEvidenceSource)
				if err != nil {
					t.Fatalf("cross-repo WriteEdges() error = %v", err)
				}
			}
			for _, writerName := range testCase.order {
				switch writerName {
				case 'w':
					workloadWrite()
				case 'x':
					crossRepoWrite()
				}
			}

			got := readRunsOnTuple(t, runner, fixture)
			if got["confidence"] != testCase.wantConf || got["reason"] != testCase.wantReason ||
				got["evidence_source"] != testCase.wantSource || got["source_tool"] != testCase.wantTool {
				t.Fatalf("RUNS_ON tuple = %#v, want confidence=%v reason=%q evidence_source=%q source_tool=%#v",
					got, testCase.wantConf, testCase.wantReason, testCase.wantSource, testCase.wantTool)
			}
		})
	}
}

func TestBoltRunsOnAtomicGroupRollsBackAfterFirstStatement(t *testing.T) {
	runner := openRunsOnBoltTestRunner(t)
	ctx := context.Background()
	t.Cleanup(func() { runner.close(ctx) })
	fixture := newRunsOnWriterFixture(t, runner, "rollback")
	firstStatementExecuted := make(chan struct{}, 1)

	_, err := materializeRunsOn(ctx, runsOnBoltExecutor{
		runner:            runner,
		groupGate:         &runsOnGroupGate{afterStatement: 0, reached: firstStatementExecuted},
		forceInvalidFinal: true,
	}, fixture)
	if err == nil {
		t.Fatal("Materialize() error = nil, want forced second-statement failure")
	}
	awaitRunsOnGate(t, firstStatementExecuted, "successful first statement before forced failure")
	if rows := readRunsOnRows(t, runner, fixture); len(rows) != 0 {
		t.Fatalf("RUNS_ON rows after failed atomic group = %#v, want none", rows)
	}

	if _, err := materializeRunsOn(ctx, runsOnBoltExecutor{runner: runner}, fixture); err != nil {
		t.Fatalf("retry Materialize(): %v", err)
	}
	assertRunsOnCrossRepoOrWorkloadTuple(t, readRunsOnTuple(t, runner, fixture), reducer.EvidenceSourceWorkloads)
}

func writeCrossRepoRunsOn(
	ctx context.Context,
	executor runsOnBoltExecutor,
	fixture runsOnWriterFixture,
) error {
	_, err := NewEdgeWriter(executor, 1).WriteEdges(
		ctx,
		reducer.DomainRepoDependency,
		[]reducer.SharedProjectionIntentRow{{
			IntentID:     "pr6634-legacy-replacement-" + fixture.instanceID,
			RepositoryID: fixture.repoID,
			Payload: map[string]any{
				"repo_id":           fixture.repoID,
				"platform_id":       fixture.platformID,
				"relationship_type": "RUNS_ON",
				"source_tool":       "argocd",
			},
		}},
		reducer.CrossRepoEvidenceSource,
	)
	return err
}

type runsOnWriterFixture struct {
	repoID     string
	workloadID string
	instanceID string
	platformID string
}

func newRunsOnWriterFixture(t *testing.T, runner *boltRetractTestRunner, suffix string) runsOnWriterFixture {
	t.Helper()
	nonce := fmt.Sprintf("%d-%s", time.Now().UTC().UnixNano(), suffix)
	fixture := runsOnWriterFixture{
		repoID:     "pr6634-runs-on-repo-" + nonce,
		workloadID: "pr6634-runs-on-workload-" + nonce,
		instanceID: "pr6634-runs-on-instance-" + nonce,
		platformID: "pr6634-runs-on-platform-" + nonce,
	}
	t.Cleanup(func() { cleanupRunsOnWriterFixture(t, runner, fixture) })
	seedRunsOnWriterFixture(t, context.Background(), runner, fixture.repoID, fixture.workloadID, fixture.instanceID, fixture.platformID)
	return fixture
}

func cleanupRunsOnWriterFixture(t *testing.T, runner *boltRetractTestRunner, fixture runsOnWriterFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ids := []string{fixture.repoID, fixture.workloadID, fixture.instanceID, fixture.platformID}
	if err := runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: `
MATCH (n)
WHERE n.id IN $ids
DETACH DELETE n`, Parameters: map[string]any{"ids": ids}}); err != nil {
		t.Errorf("clean RUNS_ON writer fixture: %v", err)
		return
	}
	rows, err := runner.runCypher(ctx, `MATCH (n) WHERE n.id IN $ids RETURN n.id AS id`, map[string]any{"ids": ids})
	if err != nil {
		t.Errorf("verify RUNS_ON writer fixture cleanup: %v", err)
		return
	}
	if len(rows) != 0 {
		t.Errorf("RUNS_ON writer fixture cleanup left rows: %#v", rows)
	}
}

func materializeRunsOn(
	ctx context.Context,
	executor runsOnBoltExecutor,
	fixture runsOnWriterFixture,
) (reducer.MaterializeResult, error) {
	return reducer.NewWorkloadMaterializer(executor).Materialize(ctx, &reducer.ProjectionResult{
		RuntimePlatformRows: []reducer.RuntimePlatformRow{{
			Confidence:   0.42,
			InstanceID:   fixture.instanceID,
			PlatformID:   fixture.platformID,
			PlatformKind: "kubernetes",
			PlatformName: "pr6634-platform",
			RepoID:       fixture.repoID,
		}},
	})
}

func readRunsOnRows(t *testing.T, runner *boltRetractTestRunner, fixture runsOnWriterFixture) []map[string]any {
	t.Helper()
	rows, err := runner.runCypher(context.Background(), `
MATCH (:WorkloadInstance {id: $instance_id})-[rel:RUNS_ON]->(:Platform {id: $platform_id})
RETURN rel.confidence AS confidence, rel.reason AS reason,
       rel.evidence_source AS evidence_source, rel.source_tool AS source_tool,
       rel.identity_key AS identity_key`, map[string]any{
		"instance_id": fixture.instanceID,
		"platform_id": fixture.platformID,
	})
	if err != nil {
		t.Fatalf("read RUNS_ON tuple: %v", err)
	}
	return rows
}

func readRunsOnTuple(t *testing.T, runner *boltRetractTestRunner, fixture runsOnWriterFixture) map[string]any {
	t.Helper()
	rows := readRunsOnRows(t, runner, fixture)
	if len(rows) != 1 {
		t.Fatalf("RUNS_ON rows = %d, want 1: %#v", len(rows), rows)
	}
	return rows[0]
}

func assertRunsOnCrossRepoOrWorkloadTuple(t *testing.T, got map[string]any, source string) {
	t.Helper()
	wantConfidence := 0.42
	wantReason := "Workload instance runs on inferred platform"
	var wantTool any
	if source == reducer.CrossRepoEvidenceSource {
		wantConfidence = 0.97
		wantReason = "Repository workload instance runs on inferred platform"
		wantTool = "argocd"
	}
	if got["confidence"] != wantConfidence || got["reason"] != wantReason ||
		got["evidence_source"] != source || got["source_tool"] != wantTool ||
		got["identity_key"] != "canonical" {
		t.Fatalf("RUNS_ON tuple = %#v, want confidence=%v reason=%q evidence_source=%q source_tool=%#v identity_key=canonical",
			got, wantConfidence, wantReason, source, wantTool)
	}
}

func openRunsOnBoltTestRunner(tb testing.TB) *boltRetractTestRunner {
	tb.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		tb.Skip("ESHU_CYPHER_BOLT_DSN not set; skipping bolt integration test")
	}
	auth := neo4jdriver.NoAuth()
	if username := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USERNAME")); username != "" {
		auth = neo4jdriver.BasicAuth(username, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	driver, err := neo4jdriver.NewDriverWithContext(dsn, auth)
	if err != nil {
		tb.Fatalf("open bolt driver %q: %v", dsn, err)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))
	if database == "" {
		database = "nornic"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		tb.Fatalf("verify bolt connectivity %q: %v", dsn, err)
	}
	return &boltRetractTestRunner{driver: driver, databaseName: database}
}

func seedRunsOnWriterFixture(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	repoID, workloadID, instanceID, platformID string,
) {
	t.Helper()
	err := runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: `
CREATE (repo:Repository {id: $repo_id})
CREATE (workload:Workload {id: $workload_id})
CREATE (instance:WorkloadInstance {id: $instance_id})
CREATE (platform:Platform {id: $platform_id})`, Parameters: map[string]any{
		"repo_id":     repoID,
		"workload_id": workloadID,
		"instance_id": instanceID,
		"platform_id": platformID,
	}})
	if err != nil {
		t.Fatalf("seed RUNS_ON writer fixture nodes: %v", err)
	}
	err = runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: `
MATCH (repo:Repository {id: $repo_id})
MATCH (workload:Workload {id: $workload_id})
MATCH (instance:WorkloadInstance {id: $instance_id})
MERGE (repo)-[:DEFINES]->(workload)
MERGE (instance)-[:INSTANCE_OF]->(workload)`, Parameters: map[string]any{
		"repo_id":     repoID,
		"workload_id": workloadID,
		"instance_id": instanceID,
	}})
	if err != nil {
		t.Fatalf("seed RUNS_ON writer fixture edges: %v", err)
	}
}
