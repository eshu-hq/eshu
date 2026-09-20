// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type runsOnPairFixture struct {
	repos     [2]string
	workloads [2]string
	instances [2]string
	platforms [2]string
}

// TestRunsOnPairIsolationLive proves that both production writers replace
// only requested RUNS_ON pairs, even when legacy and keyed neighbor edges exist.
func TestRunsOnPairIsolationLive(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if uri == "" {
		t.Skip("ESHU_CYPHER_BOLT_DSN not set; skipping isolated Bolt graph test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify Bolt connectivity: %v", err)
	}
	runner := neo4jSessionRunner{Driver: driver, DatabaseName: "nornic", TxTimeout: 30 * time.Second}

	for _, tc := range []struct {
		name  string
		order string
	}{
		{name: "workload_then_cross_repo", order: "wx"},
		{name: "cross_repo_then_workload", order: "xw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRunsOnPairFixture(t)
			seedRunsOnPairFixture(t, ctx, runner, fixture)
			assertRunsOnPairFixture(t, ctx, runner, fixture, "seeded")

			materializer := reducer.NewWorkloadMaterializer(newReducerCypherExecutor(runner, nil))
			edgeWriter := edgewriter.NewEdgeWriter(newReducerNeo4jExecutor(runner, nil), 2)
			writeWorkloads := func() {
				t.Helper()
				rows := make([]reducer.RuntimePlatformRow, 0, 2)
				for index := range fixture.instances {
					rows = append(rows, reducer.RuntimePlatformRow{
						Confidence:   0.42,
						InstanceID:   fixture.instances[index],
						PlatformID:   fixture.platforms[index],
						PlatformKind: "kubernetes",
						PlatformName: "pair-isolation",
						RepoID:       fixture.repos[index],
					})
				}
				if _, err := materializer.Materialize(ctx, &reducer.ProjectionResult{RuntimePlatformRows: rows}); err != nil {
					t.Fatalf("workload Materialize(): %v", err)
				}
			}
			writeCrossRepo := func() {
				t.Helper()
				rows := make([]reducer.SharedProjectionIntentRow, 0, 2)
				for index := range fixture.repos {
					rows = append(rows, reducer.SharedProjectionIntentRow{
						IntentID:     fixture.repos[index] + "-runs-on",
						RepositoryID: fixture.repos[index],
						Payload: map[string]any{
							"repo_id":           fixture.repos[index],
							"platform_id":       fixture.platforms[index],
							"relationship_type": "RUNS_ON",
							"source_tool":       "argocd",
						},
					})
				}
				if _, err := edgeWriter.WriteEdges(ctx, reducer.DomainRepoDependency, rows, reducer.CrossRepoEvidenceSource); err != nil {
					t.Fatalf("cross-repo WriteEdges(): %v", err)
				}
			}
			for pass := 0; pass < 2; pass++ {
				for _, writer := range tc.order {
					switch writer {
					case 'w':
						writeWorkloads()
					case 'x':
						writeCrossRepo()
					}
				}
				assertRunsOnPairFixture(t, ctx, runner, fixture, "cross_repo")
			}
		})
	}

	for _, tc := range []struct {
		name   string
		writer string
	}{
		{name: "workload_writer_synthetic_conflict", writer: "workload"},
		{name: "cross_repo_writer_synthetic_conflict", writer: "cross_repo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRunsOnPairFixture(t)
			seedRunsOnPairFixture(t, ctx, runner, fixture)
			probe := &runsOnRollbackProbe{t: t, runner: runner, fixture: fixture}
			switch tc.writer {
			case "workload":
				rows := make([]reducer.RuntimePlatformRow, 0, 2)
				for index := range fixture.instances {
					rows = append(rows, reducer.RuntimePlatformRow{
						Confidence:   0.42,
						InstanceID:   fixture.instances[index],
						PlatformID:   fixture.platforms[index],
						PlatformKind: "kubernetes",
						PlatformName: "pair-isolation",
						RepoID:       fixture.repos[index],
					})
				}
				materializer := reducer.NewWorkloadMaterializer(newReducerCypherExecutor(probe, nil))
				if _, err := materializer.Materialize(ctx, &reducer.ProjectionResult{RuntimePlatformRows: rows}); err != nil {
					t.Fatalf("workload Materialize() after synthetic conflict: %v", err)
				}
				assertRunsOnPairFixture(t, ctx, runner, fixture, "workload")
			case "cross_repo":
				rows := make([]reducer.SharedProjectionIntentRow, 0, 2)
				for index := range fixture.repos {
					rows = append(rows, reducer.SharedProjectionIntentRow{
						IntentID:     fixture.repos[index] + "-runs-on",
						RepositoryID: fixture.repos[index],
						Payload: map[string]any{
							"repo_id":           fixture.repos[index],
							"platform_id":       fixture.platforms[index],
							"relationship_type": "RUNS_ON",
							"source_tool":       "argocd",
						},
					})
				}
				writer := edgewriter.NewEdgeWriter(newReducerNeo4jExecutor(probe, nil), 2)
				if _, err := writer.WriteEdges(ctx, reducer.DomainRepoDependency, rows, reducer.CrossRepoEvidenceSource); err != nil {
					t.Fatalf("cross-repo WriteEdges() after synthetic conflict: %v", err)
				}
				assertRunsOnPairFixture(t, ctx, runner, fixture, "cross_repo")
			}
			if probe.groupAttempts != 2 {
				t.Fatalf("group attempts = %d, want failed managed transaction and one replay", probe.groupAttempts)
			}
		})
	}
}

// runsOnRollbackProbe injects a statement error inside the real Bolt group on
// its first attempt. The subsequent typed conflict is synthetic: it exercises
// Eshu's outer retry classification, not NornicDB's natural conflict timing.
type runsOnRollbackProbe struct {
	t             *testing.T
	runner        neo4jSessionRunner
	fixture       runsOnPairFixture
	groupAttempts int
}

func (p *runsOnRollbackProbe) RunCypher(ctx context.Context, statement string, params map[string]any) error {
	return p.runner.RunCypher(ctx, statement, params)
}

func (p *runsOnRollbackProbe) RunCypherGroup(ctx context.Context, statements []cypher.Statement) error {
	p.groupAttempts++
	if p.groupAttempts != 1 {
		return p.runner.RunCypherGroup(ctx, statements)
	}
	runsOnStatements := 0
	for _, statement := range statements {
		if strings.Contains(statement.Cypher, "RUNS_ON") {
			runsOnStatements++
		}
	}
	if runsOnStatements < 2 {
		return fmt.Errorf("first group has %d RUNS_ON statements, want cleanup and upsert", runsOnStatements)
	}
	session := p.runner.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: p.runner.DatabaseName,
	})
	defer func() { _ = session.Close(context.Background()) }()
	executed := 0
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		executed = 0
		for _, statement := range statements {
			result, runErr := tx.Run(ctx, statement.Cypher, statement.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return nil, consumeErr
			}
			executed++
		}
		result, runErr := tx.Run(ctx, "THIS IS NOT VALID CYPHER", nil)
		if runErr != nil {
			return nil, runErr
		}
		_, consumeErr := result.Consume(ctx)
		return nil, consumeErr
	})
	if executed != len(statements) {
		return fmt.Errorf("ran %d/%d original statements before forced failure: %w", executed, len(statements), err)
	}
	if err == nil {
		return fmt.Errorf("invalid final statement unexpectedly committed")
	}
	assertRunsOnPairFixture(p.t, ctx, p.runner, p.fixture, "seeded")
	return &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Statement.SyntaxError",
		Msg:  "UNWIND MERGE chain relationship update failed: not found",
	}
}

func newRunsOnPairFixture(t *testing.T) runsOnPairFixture {
	t.Helper()
	prefix := fmt.Sprintf("pr6634-pair-%d", time.Now().UnixNano())
	return runsOnPairFixture{
		repos:     [2]string{prefix + "-repo-a", prefix + "-repo-b"},
		workloads: [2]string{prefix + "-workload-a", prefix + "-workload-b"},
		instances: [2]string{prefix + "-instance-a", prefix + "-instance-b"},
		platforms: [2]string{prefix + "-platform-a", prefix + "-platform-b"},
	}
}

func seedRunsOnPairFixture(t *testing.T, ctx context.Context, runner neo4jSessionRunner, fixture runsOnPairFixture) {
	t.Helper()
	ids := []string{
		fixture.repos[0], fixture.repos[1],
		fixture.workloads[0], fixture.workloads[1],
		fixture.instances[0], fixture.instances[1],
		fixture.platforms[0], fixture.platforms[1],
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runner.RunCypher(cleanupCtx,
			`MATCH (n) WHERE n.id IN $ids DETACH DELETE n`, map[string]any{"ids": ids}); err != nil {
			t.Errorf("clean RUNS_ON pair fixture: %v", err)
		}
	})
	for index := range fixture.repos {
		runRunsOnPairCypher(t, ctx, runner, `
CREATE (repo:Repository {id: $repo_id})
CREATE (workload:Workload {id: $workload_id})
CREATE (instance:WorkloadInstance {id: $instance_id})
CREATE (platform:Platform {id: $platform_id})`, map[string]any{
			"repo_id": fixture.repos[index], "workload_id": fixture.workloads[index],
			"instance_id": fixture.instances[index], "platform_id": fixture.platforms[index],
		})
		runRunsOnPairCypher(t, ctx, runner, `
MATCH (repo:Repository {id: $repo_id})
MATCH (workload:Workload {id: $workload_id})
MATCH (instance:WorkloadInstance {id: $instance_id})
MERGE (repo)-[:DEFINES]->(workload)
MERGE (instance)-[:INSTANCE_OF]->(workload)`, map[string]any{
			"repo_id": fixture.repos[index], "workload_id": fixture.workloads[index],
			"instance_id": fixture.instances[index],
		})
		runRunsOnPairCypher(t, ctx, runner, `
MATCH (instance:WorkloadInstance {id: $instance_id})
MATCH (platform:Platform {id: $platform_id})
CREATE (instance)-[first:RUNS_ON]->(platform)
CREATE (instance)-[second:RUNS_ON]->(platform)
SET first.evidence_source = 'legacy/diagonal',
    second.evidence_source = 'legacy/diagonal'`, map[string]any{
			"instance_id": fixture.instances[index], "platform_id": fixture.platforms[index],
		})
	}
	for index := range fixture.instances {
		runRunsOnPairCypher(t, ctx, runner, `
MATCH (instance:WorkloadInstance {id: $instance_id})
MATCH (platform:Platform {id: $platform_id})
CREATE (instance)-[rel:RUNS_ON]->(platform)
SET rel.evidence_source = 'legacy/off-diagonal'`, map[string]any{
			"instance_id": fixture.instances[index], "platform_id": fixture.platforms[1-index],
		})
	}
	runRunsOnPairCypher(t, ctx, runner, `
MATCH (instance:WorkloadInstance {id: $instance_id})
MATCH (platform:Platform {id: $platform_id})
CREATE (instance)-[rel:RUNS_ON {identity_key: 'canonical'}]->(platform)
SET rel.confidence = 0.61,
    rel.reason = 'foreign pair',
    rel.evidence_source = 'foreign/off-diagonal',
    rel.source_tool = 'foreign-tool'`, map[string]any{
		"instance_id": fixture.instances[0], "platform_id": fixture.platforms[1],
	})
}

func runRunsOnPairCypher(t *testing.T, ctx context.Context, runner neo4jSessionRunner, statement string, params map[string]any) {
	t.Helper()
	if err := runner.RunCypher(ctx, statement, params); err != nil {
		t.Fatalf("seed RUNS_ON pair fixture: %v", err)
	}
}

func assertRunsOnPairFixture(t *testing.T, ctx context.Context, runner neo4jSessionRunner, fixture runsOnPairFixture, state string) {
	t.Helper()
	for source := range fixture.instances {
		for target := range fixture.platforms {
			rows, err := runner.Run(ctx, `
MATCH (instance:WorkloadInstance {id: $instance_id})-[rel:RUNS_ON]->(platform:Platform {id: $platform_id})
RETURN rel.identity_key AS identity_key, rel.confidence AS confidence,
       rel.reason AS reason, rel.evidence_source AS evidence_source,
       rel.source_tool AS source_tool`, map[string]any{
				"instance_id": fixture.instances[source], "platform_id": fixture.platforms[target],
			})
			if err != nil {
				t.Fatalf("read RUNS_ON pair %d/%d: %v", source, target, err)
			}
			if source == target {
				if state == "seeded" {
					if len(rows) != 2 {
						t.Fatalf("seeded diagonal RUNS_ON pair %d = %#v, want two legacy edges", source, rows)
					}
					for _, row := range rows {
						if row["identity_key"] != nil || row["evidence_source"] != "legacy/diagonal" {
							t.Fatalf("seeded diagonal RUNS_ON pair %d changed: %#v", source, rows)
						}
					}
					continue
				}
				if state == "workload" {
					assertRunsOnPairTuple(t, rows, "canonical", 0.42,
						"Workload instance runs on inferred platform", reducer.EvidenceSourceWorkloads, nil)
					continue
				}
				assertRunsOnPairTuple(t, rows, "canonical", 0.97,
					"Repository workload instance runs on inferred platform",
					reducer.CrossRepoEvidenceSource, "argocd")
				continue
			}
			if source == 0 {
				if len(rows) != 2 {
					t.Fatalf("off-diagonal RUNS_ON pair 0/1 = %#v, want legacy and foreign keyed edges", rows)
				}
				var legacy, foreign bool
				for _, row := range rows {
					switch row["evidence_source"] {
					case "legacy/off-diagonal":
						legacy = row["identity_key"] == nil
					case "foreign/off-diagonal":
						foreign = row["identity_key"] == "canonical" && row["confidence"] == 0.61 &&
							row["reason"] == "foreign pair" && row["source_tool"] == "foreign-tool"
					}
				}
				if !legacy || !foreign {
					t.Fatalf("off-diagonal RUNS_ON pair 0/1 changed: %#v", rows)
				}
				continue
			}
			assertRunsOnPairTuple(t, rows, nil, nil, nil, "legacy/off-diagonal", nil)
		}
	}
}

func assertRunsOnPairTuple(t *testing.T, rows []map[string]any, identity, confidence, reason, source, tool any) {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("RUNS_ON pair = %#v, want exactly one edge", rows)
	}
	row := rows[0]
	if row["identity_key"] != identity || row["confidence"] != confidence ||
		row["reason"] != reason || row["evidence_source"] != source || row["source_tool"] != tool {
		t.Fatalf("RUNS_ON tuple = %#v, want identity=%#v confidence=%#v reason=%#v source=%#v tool=%#v",
			row, identity, confidence, reason, source, tool)
	}
}
