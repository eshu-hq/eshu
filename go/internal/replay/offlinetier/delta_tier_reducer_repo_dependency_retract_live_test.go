// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized repository relationship edge retract coverage (C-14
// #4367 retract axis).
//
// This is the regression test for the repo-dependency sibling of the #5116
// grouped-transaction defect: executeRepoDependencyRetractStatements dispatched
// its three retract statements (typed repository relationships, RUNS_ON, and
// the evidence-artifact DETACH DELETE) through ExecuteGroup — one managed Bolt
// transaction. On the pinned NornicDB v1.1.11 multiple DELETE statements
// sharing one managed transaction under-apply (measured on the SQL retract in
// #5128: the first grouped DELETE never applied), so a repo refresh left stale
// relationship edges behind. The statement shapes themselves are safe
// (single-label Repository anchors, relationship-type disjunction).
//
// The test drives the REAL production write and retract paths
// (cypher.EdgeWriter.WriteEdges / RetractEdges for
// reducer.DomainRepoDependency). It writes every relationship type the domain
// can produce — the six typed repository relationships (DEPENDS_ON,
// DEPLOYS_FROM, DISCOVERS_CONFIG_IN, PROVISIONS_DEPENDENCY_FOR, USES_MODULE,
// READS_CONFIG_FROM), RUNS_ON (WorkloadInstance->Platform), and the
// evidence-artifact family (HAS_DEPLOYMENT_EVIDENCE,
// EVIDENCES_REPOSITORY_RELATIONSHIP, TARGETS_ENVIRONMENT) — plus an
// out-of-scope DEPENDS_ON edge, retracts the in-scope repository, and asserts
// every in-scope edge is gone, the out-of-scope edge survives, and every
// endpoint node except the intentionally DETACH-deleted EvidenceArtifact
// survives.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	repoDepInSrc     = "replay-repodep:in-src"
	repoDepInTgt     = "replay-repodep:in-tgt"
	repoDepOutSrc    = "replay-repodep:out-src"
	repoDepOutTgt    = "replay-repodep:out-tgt"
	repoDepWorkload  = "replay-repodep:workload"
	repoDepInstance  = "replay-repodep:instance"
	repoDepPlatform  = "replay-repodep:platform"
	repoDepEnv       = "replay-repodep-env"
	repoDepEvidence  = "finalization/workloads"
	repoDepMarkerKey = "replay-repodep"
)

var repoDepTypedRelationships = []string{
	"DEPENDS_ON", "DEPLOYS_FROM", "DISCOVERS_CONFIG_IN",
	"PROVISIONS_DEPENDENCY_FOR", "USES_MODULE", "READS_CONFIG_FROM",
}

// TestReducerRepoDependencyEdgeRetractGraphTruth proves the repo-dependency
// retract deletes every edge family the domain writes, on a real NornicDB. It
// is the failing-then-green regression for the grouped-transaction defect in
// executeRepoDependencyRetractStatements.
func TestReducerRepoDependencyEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the repo-dependency retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupRepoDependencyScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupRepoDependencyScope(cleanCtx, t, exec)
	})

	seedRepoDependencyNodes(ctx, t, exec)

	writer := cypher.NewEdgeWriter(exec, 0)

	writeRows := make([]reducer.SharedProjectionIntentRow, 0, len(repoDepTypedRelationships)+3)
	for _, relType := range repoDepTypedRelationships {
		writeRows = append(writeRows, reducer.SharedProjectionIntentRow{
			IntentID: "rel-" + relType, RepositoryID: repoDepInSrc, Payload: map[string]any{
				"repo_id": repoDepInSrc, "target_repo_id": repoDepInTgt,
				"relationship_type": relType,
			},
		})
	}
	writeRows = append(writeRows,
		reducer.SharedProjectionIntentRow{
			IntentID: "runs-on", RepositoryID: repoDepInSrc, Payload: map[string]any{
				"repo_id": repoDepInSrc, "relationship_type": "RUNS_ON",
				"platform_id": repoDepPlatform,
			},
		},
		reducer.SharedProjectionIntentRow{
			IntentID: "evidence", RepositoryID: repoDepInSrc, GenerationID: "gen-1", Payload: map[string]any{
				"repo_id": repoDepInSrc, "target_repo_id": repoDepInTgt,
				"relationship_type": "DEPENDS_ON", "resolved_id": "resolved-1",
				"evidence_artifacts": []any{map[string]any{
					"evidence_kind": "helm_values", "path": "deploy/values.yaml",
					"matched_value": repoDepMarkerKey, "environment": repoDepEnv,
					"confidence": 0.9,
				}},
			},
		},
		reducer.SharedProjectionIntentRow{
			IntentID: "depends-out", RepositoryID: repoDepOutSrc, Payload: map[string]any{
				"repo_id": repoDepOutSrc, "target_repo_id": repoDepOutTgt,
			},
		},
	)
	if err := writer.WriteEdges(ctx, reducer.DomainRepoDependency, writeRows, repoDepEvidence); err != nil {
		t.Fatalf("WriteEdges: %v", err)
	}

	relQ := func(relType string) string {
		return "MATCH (:Repository {id: $s})-[r:" + relType + "]->(:Repository {id: $t}) RETURN count(r)"
	}
	inPair := map[string]any{"s": repoDepInSrc, "t": repoDepInTgt}
	outPair := map[string]any{"s": repoDepOutSrc, "t": repoDepOutTgt}
	runsOnQ := "MATCH (:WorkloadInstance {uid: $i})-[r:RUNS_ON]->(:Platform {id: $p}) RETURN count(r)"
	runsOnP := map[string]any{"i": repoDepInstance, "p": repoDepPlatform}
	hasEvQ := "MATCH (:Repository {id: $s})-[r:HAS_DEPLOYMENT_EVIDENCE]->(:EvidenceArtifact) RETURN count(r)"
	evRelQ := "MATCH (:EvidenceArtifact)-[r:EVIDENCES_REPOSITORY_RELATIONSHIP]->(:Repository {id: $t}) RETURN count(r)"
	envQ := "MATCH (:EvidenceArtifact)-[r:TARGETS_ENVIRONMENT]->(:Environment {name: $e}) RETURN count(r)"
	nodeQ := "MATCH (n {id: $u}) RETURN count(n)"

	for _, relType := range repoDepTypedRelationships {
		assertEdgeCount(ctx, t, exec, relQ(relType), inPair, 1, "write: in-scope "+relType+" present")
	}
	assertEdgeCount(ctx, t, exec, runsOnQ, runsOnP, 1, "write: in-scope RUNS_ON present")
	assertEdgeCount(ctx, t, exec, hasEvQ, map[string]any{"s": repoDepInSrc}, 1, "write: HAS_DEPLOYMENT_EVIDENCE present")
	assertEdgeCount(ctx, t, exec, evRelQ, map[string]any{"t": repoDepInTgt}, 1, "write: EVIDENCES_REPOSITORY_RELATIONSHIP present")
	assertEdgeCount(ctx, t, exec, envQ, map[string]any{"e": repoDepEnv}, 1, "write: TARGETS_ENVIRONMENT present")
	assertEdgeCount(ctx, t, exec, relQ("DEPENDS_ON"), outPair, 1, "write: out-of-scope DEPENDS_ON present")

	retractRows := []reducer.SharedProjectionIntentRow{
		{IntentID: "retract", RepositoryID: repoDepInSrc, Payload: map[string]any{"repo_id": repoDepInSrc}},
	}
	if err := writer.RetractEdges(ctx, reducer.DomainRepoDependency, retractRows, repoDepEvidence); err != nil {
		t.Fatalf("RetractEdges: %v", err)
	}

	// Grouped-transaction fix: every in-scope edge family must be gone.
	for _, relType := range repoDepTypedRelationships {
		assertEdgeCount(ctx, t, exec, relQ(relType), inPair, 0, "retract: in-scope "+relType+" gone")
	}
	assertEdgeCount(ctx, t, exec, runsOnQ, runsOnP, 0, "retract: in-scope RUNS_ON gone")
	assertEdgeCount(ctx, t, exec, hasEvQ, map[string]any{"s": repoDepInSrc}, 0, "retract: HAS_DEPLOYMENT_EVIDENCE gone")
	assertEdgeCount(ctx, t, exec, evRelQ, map[string]any{"t": repoDepInTgt}, 0, "retract: EVIDENCES_REPOSITORY_RELATIONSHIP gone")
	assertEdgeCount(ctx, t, exec, envQ, map[string]any{"e": repoDepEnv}, 0, "retract: TARGETS_ENVIRONMENT gone")
	// Scoped retract, not a wipe: the out-of-scope repository's edge survives.
	assertEdgeCount(ctx, t, exec, relQ("DEPENDS_ON"), outPair, 1, "retract: out-of-scope DEPENDS_ON survives")
	// Endpoint nodes survive; the EvidenceArtifact is intentionally
	// DETACH-deleted by the retract (bounded provenance node), and the
	// Environment node stays.
	for _, id := range []string{repoDepInSrc, repoDepInTgt, repoDepOutSrc, repoDepOutTgt, repoDepPlatform} {
		assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": id}, 1, "node survives: "+id)
	}
	assertEdgeCount(ctx, t, exec, "MATCH (n:Environment {name: $u}) RETURN count(n)", map[string]any{"u": repoDepEnv}, 1, "node survives: environment")
	assertEdgeCount(ctx, t, exec, "MATCH (n:WorkloadInstance {uid: $u}) RETURN count(n)", map[string]any{"u": repoDepInstance}, 1, "node survives: instance")
	assertEdgeCount(ctx, t, exec, "MATCH (a:EvidenceArtifact {matched_value: $u}) RETURN count(a)", map[string]any{"u": repoDepMarkerKey}, 0, "evidence artifact detach-deleted")
}

// seedRepoDependencyNodes creates the Repository pairs, the
// Repository-DEFINES->Workload<-INSTANCE_OF-WorkloadInstance chain the RUNS_ON
// write MATCHes, and the Platform node.
func seedRepoDependencyNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE (inSrc:Repository {id: $inSrc, marker: $marker}),
       (:Repository {id: $inTgt, marker: $marker}),
       (:Repository {id: $outSrc, marker: $marker}),
       (:Repository {id: $outTgt, marker: $marker}),
       (w:Workload {id: $workload, marker: $marker}),
       (i:WorkloadInstance {uid: $instance, marker: $marker}),
       (:Platform {id: $platform, marker: $marker}),
       (inSrc)-[:DEFINES]->(w),
       (i)-[:INSTANCE_OF]->(w)`,
		Parameters: map[string]any{
			"inSrc": repoDepInSrc, "inTgt": repoDepInTgt,
			"outSrc": repoDepOutSrc, "outTgt": repoDepOutTgt,
			"workload": repoDepWorkload, "instance": repoDepInstance,
			"platform": repoDepPlatform, "marker": repoDepMarkerKey,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed repo-dependency nodes: %v", err)
	}
}

// cleanupRepoDependencyScope removes every node this test creates (seeded
// marker nodes, the write-MERGEd EvidenceArtifact and Environment) so a rerun
// starts clean.
func cleanupRepoDependencyScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, stmt := range []cypher.Statement{
		{
			Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
			Parameters: map[string]any{"marker": repoDepMarkerKey},
		},
		{
			Cypher:     `MATCH (a:EvidenceArtifact {matched_value: $marker}) DETACH DELETE a`,
			Parameters: map[string]any{"marker": repoDepMarkerKey},
		},
		{
			Cypher:     `MATCH (e:Environment {name: $env}) DETACH DELETE e`,
			Parameters: map[string]any{"env": repoDepEnv},
		},
	} {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup repo-dependency scope: %v", err)
		}
	}
}
