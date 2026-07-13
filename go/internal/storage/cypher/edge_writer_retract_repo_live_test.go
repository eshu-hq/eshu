// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBoltRepoDependencyCodeImportRetractPreservesOtherSourceRunsOn(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })

	const (
		sourceRepoID = "repository:5208-live-source"
		targetRepoID = "repository:5208-live-target"
		workloadID   = "workload:5208-live"
		instanceID   = "workload-instance:5208-live"
		platformID   = "platform:5208-live"
		artifactID   = "evidence-artifact:5208-live"
		codeImport   = "projection/code-imports"
		crossRepo    = "resolver/cross-repo"
	)
	ctx := context.Background()
	ids := []string{sourceRepoID, targetRepoID, workloadID, instanceID, platformID, artifactID}
	t.Cleanup(func() {
		if err := boltWriteStatement(
			context.Background(),
			runner,
			`MATCH (n) WHERE n.id IN $ids DETACH DELETE n`,
			map[string]any{"ids": ids},
		); err != nil {
			t.Errorf("cleanup live repo-dependency proof: %v", err)
		}
	})

	nodeSeeds := []struct {
		name   string
		cypher string
		params map[string]any
	}{
		{name: "source repository", cypher: `MERGE (:Repository {id: $id})`, params: map[string]any{"id": sourceRepoID}},
		{name: "target repository", cypher: `MERGE (:Repository {id: $id})`, params: map[string]any{"id": targetRepoID}},
		{name: "workload", cypher: `MERGE (:Workload {id: $id})`, params: map[string]any{"id": workloadID}},
		{name: "workload instance", cypher: `MERGE (i:WorkloadInstance {id: $id}) SET i.repo_id = $repo_id`, params: map[string]any{"id": instanceID, "repo_id": sourceRepoID}},
		{name: "platform", cypher: `MERGE (:Platform {id: $id})`, params: map[string]any{"id": platformID}},
		{name: "evidence artifact", cypher: `MERGE (:EvidenceArtifact {id: $id})`, params: map[string]any{"id": artifactID}},
	}
	for _, seed := range nodeSeeds {
		if err := boltWriteStatement(ctx, runner, seed.cypher, seed.params); err != nil {
			t.Fatalf("seed live repo-dependency %s node: %v", seed.name, err)
		}
	}
	seedStatements := []struct {
		name   string
		cypher string
		params map[string]any
	}{
		{
			name:   "repository workload",
			cypher: `MATCH (source:Repository {id: $source_repo_id}) MATCH (workload:Workload {id: $workload_id}) MERGE (source)-[:DEFINES]->(workload)`,
			params: map[string]any{"source_repo_id": sourceRepoID, "workload_id": workloadID},
		},
		{
			name:   "workload instance",
			cypher: `MATCH (instance:WorkloadInstance {id: $instance_id}) MATCH (workload:Workload {id: $workload_id}) MERGE (instance)-[:INSTANCE_OF]->(workload)`,
			params: map[string]any{"instance_id": instanceID, "workload_id": workloadID},
		},
		{
			name:   "code-import dependency",
			cypher: `MATCH (source:Repository {id: $source_repo_id}) MATCH (target:Repository {id: $target_repo_id}) MERGE (source)-[r:DEPENDS_ON]->(target) SET r.evidence_source = $evidence_source`,
			params: map[string]any{"source_repo_id": sourceRepoID, "target_repo_id": targetRepoID, "evidence_source": codeImport},
		},
		{
			name:   "other-source runtime",
			cypher: `MATCH (instance:WorkloadInstance {id: $instance_id}) MATCH (platform:Platform {id: $platform_id}) MERGE (instance)-[r:RUNS_ON]->(platform) SET r.evidence_source = $evidence_source`,
			params: map[string]any{"instance_id": instanceID, "platform_id": platformID, "evidence_source": crossRepo},
		},
		{
			name:   "code-import artifact",
			cypher: `MATCH (source:Repository {id: $source_repo_id}) MATCH (artifact:EvidenceArtifact {id: $artifact_id}) MERGE (source)-[r:HAS_DEPLOYMENT_EVIDENCE]->(artifact) SET r.evidence_source = $evidence_source`,
			params: map[string]any{"source_repo_id": sourceRepoID, "artifact_id": artifactID, "evidence_source": codeImport},
		},
	}
	for _, seed := range seedStatements {
		if err := boltWriteStatement(ctx, runner, seed.cypher, seed.params); err != nil {
			t.Fatalf("seed live repo-dependency %s relationship: %v", seed.name, err)
		}
	}
	assertBoltCount(t, ctx, runner, `
MATCH (:WorkloadInstance {id: $instance_id})-[r:RUNS_ON]->(:Platform {id: $platform_id})
WHERE r.evidence_source = $evidence_source
RETURN count(r) AS count`, map[string]any{
		"instance_id":     instanceID,
		"platform_id":     platformID,
		"evidence_source": crossRepo,
	}, 1, "seeded other-source RUNS_ON")

	writer := NewEdgeWriter(&boltTestExecutor{runner: runner}, 0)
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "5208-live-retract",
		RepositoryID: sourceRepoID,
		Payload:      map[string]any{"repo_id": sourceRepoID},
	}}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := writer.RetractEdges(
			ctx,
			reducer.DomainRepoDependency,
			rows,
			codeImport,
		); err != nil {
			t.Fatalf("code-import retract attempt %d: %v", attempt, err)
		}
		assertRepoDependencyCodeImportRetractTruth(
			t,
			ctx,
			runner,
			sourceRepoID,
			targetRepoID,
			instanceID,
			platformID,
			codeImport,
			crossRepo,
		)
	}
}

func assertRepoDependencyCodeImportRetractTruth(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	sourceRepoID string,
	targetRepoID string,
	instanceID string,
	platformID string,
	codeImport string,
	crossRepo string,
) {
	t.Helper()
	assertBoltCount(t, ctx, runner, `
MATCH (:Repository {id: $source_repo_id})-[r:DEPENDS_ON]->(:Repository {id: $target_repo_id})
WHERE r.evidence_source = $evidence_source
RETURN count(r) AS count`, map[string]any{
		"source_repo_id":  sourceRepoID,
		"target_repo_id":  targetRepoID,
		"evidence_source": codeImport,
	}, 0, "code-import DEPENDS_ON")
	assertBoltCount(t, ctx, runner, `
MATCH (:WorkloadInstance {id: $instance_id})-[r:RUNS_ON]->(:Platform {id: $platform_id})
WHERE r.evidence_source = $evidence_source
RETURN count(r) AS count`, map[string]any{
		"instance_id":     instanceID,
		"platform_id":     platformID,
		"evidence_source": crossRepo,
	}, 1, "other-source RUNS_ON")
	assertBoltCount(t, ctx, runner, `
MATCH (:Repository {id: $source_repo_id})-[:HAS_DEPLOYMENT_EVIDENCE]->(:EvidenceArtifact)
RETURN count(*) AS count`, map[string]any{
		"source_repo_id": sourceRepoID,
	}, 0, "code-import evidence artifact")
}

func assertBoltCount(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	query string,
	params map[string]any,
	want int64,
	label string,
) {
	t.Helper()
	got, err := boltCount(ctx, runner, query, params)
	if err != nil {
		t.Fatalf("count %s: %v", label, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", label, got, want)
	}
}
