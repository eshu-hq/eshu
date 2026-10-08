// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestLiveOrphanSweepReachesSourcelessEvidenceArtifact is the committed live
// regression for #7322 part (2): an EvidenceArtifact whose
// HAS_DEPLOYMENT_EVIDENCE source edge is gone must be swept even when it is
// still attached to an Environment (the 426 ops-qa nodes) or a target repo.
// The repo-relationship retract reaches artifacts only through the source
// repository's edges, and the cooperation-blind S2 connectivity check treats
// any relationship as connected, so such an artifact is unreachable today.
//
// Gate: ESHU_CYPHER_BOLT_DSN must be set. When unset the test skips.
// Backend-agnostic: the sweep semantics asserted here hold identically on
// Neo4j and NornicDB (typed concrete-variable MATCH only, no negated
// relationship predicates).
func TestLiveOrphanSweepReachesSourcelessEvidenceArtifact(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sourceID := "7322-source-" + suffix
	targetID := "7322-target-" + suffix
	envName := "7322-env-" + suffix
	artifactEnvAttached := "7322-artifact-env-" + suffix
	artifactBare := "7322-artifact-bare-" + suffix
	artifactControl := "7322-artifact-control-" + suffix
	artifactIDs := []string{artifactEnvAttached, artifactBare, artifactControl}

	t.Cleanup(func() {
		if err := boltWriteStatement(
			context.Background(), runner,
			`MATCH (n) WHERE n.id IN $ids OR n.name = $env DETACH DELETE n`,
			map[string]any{"ids": append([]string{sourceID, targetID}, artifactIDs...), "env": envName},
		); err != nil {
			t.Errorf("cleanup sourceless artifact proof: %v", err)
		}
	})
	_ = boltWriteStatement(ctx, runner,
		`MATCH (n) WHERE n.id IN $ids OR n.name = $env DETACH DELETE n`,
		map[string]any{"ids": append([]string{sourceID, targetID}, artifactIDs...), "env": envName},
	)
	// Prefix-scoped pre-clean: a prior crashed run leaves marked nodes
	// behind (the fixed test clock ages them under any later run's
	// cutoff), which would inflate the exact Deleted counts below. Clear
	// every leftover carrying this test's id prefixes first; the
	// per-run cleanup above still scopes to the current run's ids.
	_ = boltWriteStatement(ctx, runner,
		`MATCH (n) WHERE n.id STARTS WITH '7322-artifact-' OR n.id STARTS WITH '7322-source-' OR n.id STARTS WITH '7322-target-' OR n.name STARTS WITH '7322-env-' DETACH DELETE n`,
		map[string]any{},
	)

	seed := func(cypher string, params map[string]any) {
		t.Helper()
		if err := boltWriteStatement(ctx, runner, cypher, params); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed(`MERGE (s:Repository {id: $source}) MERGE (t:Repository {id: $target}) MERGE (e:Environment {name: $env})`,
		map[string]any{"source": sourceID, "target": targetID, "env": envName})
	seedArtifact := func(id string) {
		t.Helper()
		seed(`MERGE (a:EvidenceArtifact {id: $id}) SET a.evidence_source = 'test-7322-live'`,
			map[string]any{"id": id})
	}
	for _, id := range artifactIDs {
		seedArtifact(id)
	}
	// Full edge set for the env-attached and control artifacts...
	seed(`MATCH (s:Repository {id: $source}) MATCH (t:Repository {id: $target})
MATCH (a:EvidenceArtifact {id: $id}) MATCH (e:Environment {name: $env})
MERGE (s)-[:HAS_DEPLOYMENT_EVIDENCE]->(a)
MERGE (a)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(t)
MERGE (a)-[:TARGETS_ENVIRONMENT]->(e)`,
		map[string]any{"source": sourceID, "target": targetID, "id": artifactEnvAttached, "env": envName})
	seed(`MATCH (s:Repository {id: $source}) MATCH (t:Repository {id: $target})
MATCH (a:EvidenceArtifact {id: $id})
MERGE (s)-[:HAS_DEPLOYMENT_EVIDENCE]->(a)
MERGE (a)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(t)`,
		map[string]any{"source": sourceID, "target": targetID, "id": artifactControl})
	// ...then the source edge of the env-attached artifact is deleted first,
	// reproducing the post-#7285-bug state: sourceless but env-attached.
	seed(`MATCH (:Repository {id: $source})-[r:HAS_DEPLOYMENT_EVIDENCE]->(:EvidenceArtifact {id: $id}) DELETE r`,
		map[string]any{"source": sourceID, "id": artifactEnvAttached})

	clock := time.Unix(1_800_000_000, 0).UTC()
	store := NewOrphanSweepStore(&boltTestExecutor{runner: runner}, &boltOrphanSweepReader{runner: runner})
	store.CountLimit = 1000
	store.Now = func() time.Time { return clock }
	policy := OrphanSweepPolicy{
		OrphanTTL:  1 * time.Second,
		BatchLimit: 100,
		CountLimit: 1000,
		Labels:     []string{"EvidenceArtifact"},
	}

	// Cycle 1: both sourceless artifacts get marked; the control keeps its
	// source edge and stays untouched.
	if _, err := store.SweepOrphanNodes(ctx, policy); err != nil {
		t.Fatalf("cycle 1 SweepOrphanNodes: %v", err)
	}
	assertArtifactMarked(t, ctx, runner, artifactEnvAttached, true)
	assertArtifactMarked(t, ctx, runner, artifactBare, true)
	assertArtifactMarked(t, ctx, runner, artifactControl, false)

	// Cycle 2 past the TTL: both sourceless artifacts are swept.
	clock = clock.Add(2 * time.Second)
	result, err := store.SweepOrphanNodes(ctx, policy)
	if err != nil {
		t.Fatalf("cycle 2 SweepOrphanNodes: %v", err)
	}
	if got := result.Deleted["EvidenceArtifact"]; got != 2 {
		t.Fatalf("cycle 2 deleted EvidenceArtifact = %d, want 2", got)
	}
	// Cycle 3 proves the sweep is idempotent: re-running it deletes
	// nothing and errors on nothing.
	rerun, err := store.SweepOrphanNodes(ctx, policy)
	if err != nil {
		t.Fatalf("cycle 3 SweepOrphanNodes: %v", err)
	}
	if got := rerun.Deleted["EvidenceArtifact"]; got != 0 {
		t.Fatalf("cycle 3 deleted EvidenceArtifact = %d, want 0", got)
	}
	assertArtifactExists(t, ctx, runner, artifactEnvAttached, false)
	assertArtifactExists(t, ctx, runner, artifactBare, false)
	assertArtifactExists(t, ctx, runner, artifactControl, true)
	// The control's source edge survived the sweep untouched.
	rows, err := runner.runCypher(ctx,
		`MATCH (:Repository {id: $source})-[r:HAS_DEPLOYMENT_EVIDENCE]->(:EvidenceArtifact {id: $id}) RETURN count(r) AS n`,
		map[string]any{"source": sourceID, "id": artifactControl})
	if err != nil {
		t.Fatalf("read back control source edge: %v", err)
	}
	if len(rows) != 1 || rows[0]["n"] != int64(1) {
		t.Fatalf("control source edge rows = %v, want one row with n=1", rows)
	}
}

func assertArtifactMarked(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, id string, want bool) {
	t.Helper()
	rows, err := runner.runCypher(ctx,
		`MATCH (n:EvidenceArtifact {id: $id}) RETURN n.eshu_orphan_observed_at_unix AS observed_at`,
		map[string]any{"id": id})
	if err != nil {
		t.Fatalf("read back artifact marker %s: %v", id, err)
	}
	if len(rows) != 1 {
		t.Fatalf("artifact %s rows = %d, want 1", id, len(rows))
	}
	marked := rows[0]["observed_at"] != nil
	if marked != want {
		t.Fatalf("artifact %s marked = %v, want %v", id, marked, want)
	}
}

func assertArtifactExists(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, id string, want bool) {
	t.Helper()
	rows, err := runner.runCypher(ctx,
		`MATCH (n:EvidenceArtifact {id: $id}) RETURN n.id AS id`,
		map[string]any{"id": id})
	if err != nil {
		t.Fatalf("read back artifact %s: %v", id, err)
	}
	if got := len(rows) == 1; got != want {
		t.Fatalf("artifact %s exists = %v, want %v", id, got, want)
	}
}
