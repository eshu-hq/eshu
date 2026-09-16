// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestBoltRunsOnConcurrentOverlapPreservesCrossRepoTuple holds one writer's
// managed transaction open while the other commits the same relationship
// identity. It proves both overlap orders converge to one edge carrying the
// higher-precedence cross-repo tuple, whether the backend blocks, retries, or
// lets the competing keyed write complete before the held commit.
func TestBoltRunsOnConcurrentOverlapPreservesCrossRepoTuple(t *testing.T) {
	runner := openRunsOnBoltTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { runner.close(context.Background()) })
	for _, testCase := range []struct {
		name       string
		startWrite func(t *testing.T, fixture runsOnWriterFixture) (<-chan error, <-chan error)
	}{
		{
			name: "workload_transaction_first",
			startWrite: func(t *testing.T, fixture runsOnWriterFixture) (<-chan error, <-chan error) {
				workloadReached := make(chan struct{}, 1)
				workloadRelease := make(chan struct{})
				t.Cleanup(func() { closeRunsOnGate(workloadRelease) })
				crossAttempted := make(chan struct{}, 1)
				crossReached := make(chan struct{}, 1)
				workloadErrors := startRunsOnWorkloadWrite(ctx, nil, runsOnBoltExecutor{
					runner: runner,
					groupGate: &runsOnGroupGate{
						afterStatement: 1,
						reached:        workloadReached,
						release:        workloadRelease,
					},
				}, fixture)
				awaitRunsOnGate(t, workloadReached, "workload identity MERGE")
				crossErrors := startRunsOnCrossRepoWrite(ctx, crossAttempted, runsOnBoltExecutor{
					runner: runner,
					groupGate: &runsOnGroupGate{
						afterStatement: 1,
						reached:        crossReached,
					},
				}, fixture)
				awaitRunsOnGate(t, crossAttempted, "cross-repo write attempt")
				observeRunsOnOverlap(t, crossReached, "cross-repo statement while workload is uncommitted")
				closeRunsOnGate(workloadRelease)
				return workloadErrors, crossErrors
			},
		},
		{
			name: "cross_repo_transaction_first",
			startWrite: func(t *testing.T, fixture runsOnWriterFixture) (<-chan error, <-chan error) {
				crossReached := make(chan struct{}, 1)
				crossRelease := make(chan struct{})
				t.Cleanup(func() { closeRunsOnGate(crossRelease) })
				workloadAttempted := make(chan struct{}, 1)
				workloadReached := make(chan struct{}, 1)
				crossErrors := startRunsOnCrossRepoWrite(ctx, nil, runsOnBoltExecutor{
					runner: runner,
					groupGate: &runsOnGroupGate{
						afterStatement: 1,
						reached:        crossReached,
						release:        crossRelease,
					},
				}, fixture)
				awaitRunsOnGate(t, crossReached, "cross-repo tuple SET")
				workloadErrors := startRunsOnWorkloadWrite(ctx, workloadAttempted, runsOnBoltExecutor{
					runner: runner,
					groupGate: &runsOnGroupGate{
						afterStatement: 1,
						reached:        workloadReached,
					},
				}, fixture)
				awaitRunsOnGate(t, workloadAttempted, "workload write attempt")
				observeRunsOnOverlap(t, workloadReached, "workload identity while cross-repo is uncommitted")
				closeRunsOnGate(crossRelease)
				return workloadErrors, crossErrors
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newRunsOnWriterFixture(t, runner, testCase.name)
			workloadErrors, crossErrors := testCase.startWrite(t, fixture)
			if err := <-workloadErrors; err != nil {
				t.Fatalf("workload write: %v", err)
			}
			if err := <-crossErrors; err != nil {
				t.Fatalf("cross-repo write: %v", err)
			}
			assertRunsOnCrossRepoOrWorkloadTuple(
				t,
				readRunsOnTuple(t, runner, fixture),
				reducer.CrossRepoEvidenceSource,
			)
		})
	}
}

func startRunsOnWorkloadWrite(
	ctx context.Context,
	attempted chan<- struct{},
	executor runsOnBoltExecutor,
	fixture runsOnWriterFixture,
) <-chan error {
	errors := make(chan error, 1)
	go func() {
		notifyRunsOnGate(attempted)
		_, err := materializeRunsOn(ctx, executor, fixture)
		errors <- err
	}()
	return errors
}

func startRunsOnCrossRepoWrite(
	ctx context.Context,
	attempted chan<- struct{},
	executor runsOnBoltExecutor,
	fixture runsOnWriterFixture,
) <-chan error {
	errors := make(chan error, 1)
	go func() {
		notifyRunsOnGate(attempted)
		_, err := NewEdgeWriter(executor, 1).WriteEdges(
			ctx,
			reducer.DomainRepoDependency,
			[]reducer.SharedProjectionIntentRow{{
				IntentID:     "pr6634-runs-on-concurrent-" + fixture.instanceID,
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
		errors <- err
	}()
	return errors
}

func awaitRunsOnGate(t *testing.T, gate <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-gate:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

// observeRunsOnOverlap distinguishes optimistic progress from backend lock
// blocking while keeping the competing transaction open in either case.
func observeRunsOnOverlap(t *testing.T, gate <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-gate:
		t.Logf("%s completed before the competing transaction committed", label)
	case <-time.After(2 * time.Second):
		t.Logf("%s remained blocked until the competing transaction committed", label)
	}
}

func closeRunsOnGate(gate chan struct{}) {
	select {
	case <-gate:
	default:
		close(gate)
	}
}
