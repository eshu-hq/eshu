// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
)

type executeOnlyRunsOnTestExecutor struct{}

func (executeOnlyRunsOnTestExecutor) ExecuteCypher(
	context.Context,
	string,
	map[string]any,
) error {
	return nil
}

func TestRuntimePlatformRunsOnRequiresAtomicGroup(t *testing.T) {
	t.Parallel()

	materializer := NewWorkloadMaterializer(executeOnlyRunsOnTestExecutor{})
	_, err := materializer.Materialize(context.Background(), &ProjectionResult{
		RuntimePlatformRows: []RuntimePlatformRow{{
			InstanceID: "instance-1",
			PlatformID: "platform-1",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "atomic") {
		t.Fatalf("Materialize() error = %v, want missing atomic-group capability", err)
	}
}

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
// The workload path therefore uses two statements: one establishes the edge
// identity, and a second MATCH updates the complete tuple only while that edge
// is unstamped or workload-owned. Cross-repo's unconditional full-tuple writer
// then wins in every possible order.
//
// The live fault-injection cell is the behavioral regression (it failed
// pre-fix and must pass post-fix); this shape test pins the mechanism so a
// template revert is caught without a live stack.
func TestRuntimePlatformRunsOnUpsertPreservesForeignStamp(t *testing.T) {
	t.Parallel()

	ensureTemplate := batchRuntimePlatformRunsOnEdgeUpsertCypher
	for _, want := range []string{
		"MERGE (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)",
	} {
		if !strings.Contains(ensureTemplate, want) {
			t.Errorf("RUNS_ON identity template missing %q:\n%s", want, ensureTemplate)
		}
	}
	if strings.Contains(ensureTemplate, "SET rel.") {
		t.Errorf("RUNS_ON identity MERGE must not mutate shared properties:\n%s", ensureTemplate)
	}
	ownedTemplate := batchRuntimePlatformRunsOnOwnedEdgePropertiesCypher
	for _, want := range []string{
		"MATCH (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)",
		"WHERE rel.evidence_source IS NULL OR rel.evidence_source = row.evidence_source",
		"rel.confidence = row.platform_confidence",
		"rel.reason = 'Workload instance runs on inferred platform'",
		"rel.evidence_source = row.evidence_source",
		"rel.source_tool = null",
	} {
		if !strings.Contains(ownedTemplate, want) {
			t.Errorf("RUNS_ON owned-property template missing %q:\n%s", want, ownedTemplate)
		}
	}
}
