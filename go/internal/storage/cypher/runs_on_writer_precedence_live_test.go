// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type runsOnBoltExecutor struct {
	runner *boltRetractTestRunner
}

func (e runsOnBoltExecutor) Execute(ctx context.Context, statement Statement) error {
	return e.runner.runCypherSingle(ctx, statement)
}

func (e runsOnBoltExecutor) ExecuteCypher(
	ctx context.Context,
	query string,
	parameters map[string]any,
) error {
	return e.Execute(ctx, Statement{Cypher: query, Parameters: parameters})
}

// TestBoltRunsOnWritersPreserveOneCoherentProvenanceTuple is the #6184
// writer-order regression for the shared WorkloadInstance-RUNS_ON-Platform
// identity. The workload materializer and cross-repo edge writer can run in
// either order, but once the cross-repo writer publishes its complete tuple a
// later workload pass must preserve all four properties together.
func TestBoltRunsOnWritersPreserveOneCoherentProvenanceTuple(t *testing.T) {
	runner := openBoltTestRunner(t)
	ctx := context.Background()
	t.Cleanup(func() { runner.close(ctx) })
	executor := runsOnBoltExecutor{runner: runner}
	runNonce := time.Now().UTC().UnixNano()

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
			repoID := fmt.Sprintf("pr6634-runs-on-repo-%d-%d", runNonce, index)
			workloadID := fmt.Sprintf("pr6634-runs-on-workload-%d-%d", runNonce, index)
			instanceID := fmt.Sprintf("pr6634-runs-on-instance-%d-%d", runNonce, index)
			platformID := fmt.Sprintf("pr6634-runs-on-platform-%d-%d", runNonce, index)
			seedRunsOnWriterFixture(t, ctx, runner, repoID, workloadID, instanceID, platformID)

			materializer := reducer.NewWorkloadMaterializer(executor)
			workloadWrite := func() {
				t.Helper()
				_, err := materializer.Materialize(ctx, &reducer.ProjectionResult{
					RuntimePlatformRows: []reducer.RuntimePlatformRow{{
						Confidence:   0.42,
						InstanceID:   instanceID,
						PlatformID:   platformID,
						PlatformKind: "kubernetes",
						PlatformName: "pr6634-platform",
						RepoID:       repoID,
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
					RepositoryID: repoID,
					Payload: map[string]any{
						"repo_id":           repoID,
						"platform_id":       platformID,
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

			rows, err := runner.runCypher(ctx, `
MATCH (:WorkloadInstance {id: $instance_id})-[rel:RUNS_ON]->(:Platform {id: $platform_id})
RETURN rel.confidence AS confidence, rel.reason AS reason,
       rel.evidence_source AS evidence_source, rel.source_tool AS source_tool`, map[string]any{
				"instance_id": instanceID,
				"platform_id": platformID,
			})
			if err != nil {
				t.Fatalf("read RUNS_ON tuple: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("RUNS_ON rows = %d, want 1: %#v", len(rows), rows)
			}
			got := rows[0]
			if got["confidence"] != testCase.wantConf || got["reason"] != testCase.wantReason ||
				got["evidence_source"] != testCase.wantSource || got["source_tool"] != testCase.wantTool {
				t.Fatalf("RUNS_ON tuple = %#v, want confidence=%v reason=%q evidence_source=%q source_tool=%#v",
					got, testCase.wantConf, testCase.wantReason, testCase.wantSource, testCase.wantTool)
			}
		})
	}
}

func seedRunsOnWriterFixture(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	repoID, workloadID, instanceID, platformID string,
) {
	t.Helper()
	err := runner.runCypherSingle(ctx, Statement{Cypher: `
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
	err = runner.runCypherSingle(ctx, Statement{Cypher: `
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
