// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestBoltRunsOnWritersReplaceDuplicateLegacyUnkeyedIdentities proves each
// production writer replaces duplicate and mixed pre-cutover identities with
// exactly one deterministic keyed relationship.
func TestBoltRunsOnWritersReplaceDuplicateLegacyUnkeyedIdentities(t *testing.T) {
	runner := openRunsOnBoltTestRunner(t)
	ctx := context.Background()
	t.Cleanup(func() { runner.close(ctx) })
	executor := runsOnBoltExecutor{runner: runner}

	testCases := []struct {
		name       string
		write      func(runsOnWriterFixture) error
		wantSource string
	}{
		{
			name: "workload_writer",
			write: func(fixture runsOnWriterFixture) error {
				_, err := materializeRunsOn(ctx, executor, fixture)
				return err
			},
			wantSource: reducer.EvidenceSourceWorkloads,
		},
		{
			name: "cross_repo_writer",
			write: func(fixture runsOnWriterFixture) error {
				return writeCrossRepoRunsOn(ctx, executor, fixture)
			},
			wantSource: reducer.CrossRepoEvidenceSource,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newRunsOnWriterFixture(t, runner, "legacy-"+testCase.name)
			seedMixedRunsOnIdentities(t, ctx, executor, fixture)

			if err := testCase.write(fixture); err != nil {
				t.Fatalf("replace legacy RUNS_ON identities: %v", err)
			}
			rows := readRunsOnRows(t, runner, fixture)
			if len(rows) != 1 {
				t.Fatalf("RUNS_ON rows after legacy replacement = %#v, want one", rows)
			}
			assertRunsOnCrossRepoOrWorkloadTuple(t, rows[0], testCase.wantSource)
		})
	}
}

func seedMixedRunsOnIdentities(
	t *testing.T,
	ctx context.Context,
	executor runsOnBoltExecutor,
	fixture runsOnWriterFixture,
) {
	t.Helper()
	err := executor.Execute(ctx, sourcecypher.Statement{
		Cypher: `MATCH (i:WorkloadInstance {id: $instance_id})
MATCH (p:Platform {id: $platform_id})
CREATE (i)-[rel1:RUNS_ON]->(p)
CREATE (i)-[rel2:RUNS_ON]->(p)
CREATE (i)-[keyed:RUNS_ON {identity_key: 'canonical'}]->(p)
SET rel1.evidence_source = $evidence_source,
    rel2.evidence_source = $evidence_source,
    keyed.confidence = 0.42,
    keyed.reason = 'Workload instance runs on inferred platform',
    keyed.evidence_source = $evidence_source`,
		Parameters: map[string]any{
			"instance_id":     fixture.instanceID,
			"platform_id":     fixture.platformID,
			"evidence_source": reducer.EvidenceSourceWorkloads,
		},
	})
	if err != nil {
		t.Fatalf("seed mixed RUNS_ON identities: %v", err)
	}
}
