// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
)

// TestRuntimePlatformRunsOnUpsertPreservesForeignStamp pins the deterministic
// writer precedence for the shared RUNS_ON identity: the cross-repo resolver
// (repo_dependency, CrossRepoEvidenceSource) and workload materialization
// (EvidenceSourceWorkloads) MERGE the same (WorkloadInstance)-[:RUNS_ON]->
// (Platform) edge, while the family's exact-set gate and its retract both key
// on the cross-repo stamp. An unconditional evidence_source SET here made the
// family's edge last-writer-wins: whenever workload materialization cycled
// after the repo lane, it restamped the family's edge and the gate reported it
// missing (#6184 live-cell wedge in killworker_repo_dependency: six repo edges
// present, RUNS_ON restamped to the workloads source).
//
// The stamp is therefore assigned only ON CREATE: the cross-repo write always
// wins when the repo lane writes, while a lane-first workload edge keeps this
// lane's stamp. Confidence and reason still refresh unconditionally, and the
// template stays CASE-free per this lane's convention (pinned by
// TestWorkloadMaterializerWritesRuntimePlatforms).
//
// The live fault-injection cell is the behavioral regression (it failed
// pre-fix and must pass post-fix); this shape test pins the mechanism so a
// template revert is caught without a live stack.
func TestRuntimePlatformRunsOnUpsertPreservesForeignStamp(t *testing.T) {
	t.Parallel()

	template := batchRuntimePlatformRunsOnEdgeUpsertCypher
	for _, want := range []string{
		"MERGE (i)-[rel:RUNS_ON]->(p)",
		"ON CREATE SET",
		"ON MATCH SET",
	} {
		if !strings.Contains(template, want) {
			t.Errorf("RUNS_ON upsert template missing %q:\n%s", want, template)
		}
	}
	// New edges are still stamped by this lane.
	head, tail, found := strings.Cut(template, "ON MATCH SET")
	if !found {
		t.Fatalf("RUNS_ON upsert template missing ON MATCH SET:\n%s", template)
	}
	if !strings.Contains(head, "rel.evidence_source = row.evidence_source") {
		t.Errorf("RUNS_ON upsert must stamp new edges on create:\n%s", head)
	}
	// Matched edges keep whatever stamp they carry: any evidence_source
	// mention past ON MATCH reintroduces the overwrite race.
	if strings.Contains(tail, "rel.evidence_source") {
		t.Errorf("RUNS_ON upsert ON MATCH tail must not touch evidence_source:\n%s", tail)
	}
}
