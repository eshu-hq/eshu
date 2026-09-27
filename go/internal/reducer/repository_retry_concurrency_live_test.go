// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package reducer_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// #7285 concurrency matrix, live. Row 7 (a B1 retract racing a same-scope
// workload_materialization) cannot happen: both are the same domain on the
// same scope, and the reducer queue's platform-graph conflict key serializes
// them (TestPlatformGraphConflictKeySameDomainSameScopeSerializes in
// storage/postgres).

// phaseHookExecutor runs statements one at a time (it deliberately does not
// implement cypher.GroupExecutor, so the writer takes its sequential path)
// and calls hook once, immediately before the first statement of phase.
type phaseHookExecutor struct {
	inner provenanceReplayExecutor
	phase string
	hook  func()
	once  *sync.Once
}

func (e phaseHookExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
	if phase, _ := stmt.Parameters[cypher.StatementMetadataPhaseKey].(string); phase == e.phase {
		e.once.Do(e.hook)
	}
	return e.inner.Execute(ctx, stmt)
}

// TestLiveRepositoryRetryInterleavedWithWorkloadMaterialization is matrix row
// 2: workload_materialization's DEFINES write lands before, between, and after
// each Repository-touching projector phase of a retry. Every placement must
// converge to the serial result. On the id-cleanup code a DEFINES placed
// before repository_cleanup is deleted, and one placed between the cleanup and
// the upsert matches no Repository and writes nothing while reporting success.
func TestLiveRepositoryRetryInterleavedWithWorkloadMaterialization(t *testing.T) {
	live := openRepoRetryLive(t)
	for _, phase := range []string{"retract", "repository_cleanup", "repository", cypher.CanonicalPhaseDirectories, cypher.CanonicalPhaseFiles, "after"} {
		t.Run(phase, func(t *testing.T) {
			live.cleanup(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			atomic := live.writerShapes()["atomic_group"]
			live.write(ctx, t, atomic, live.materialization(retryFixtureRepo, "gen-1", true))
			live.write(ctx, t, atomic, live.materialization(retryFixtureOther, "gen-1", true))
			live.seedReducerEdges(ctx, t)

			hook := func() { live.materializeRetryWorkloads(ctx, t) }
			sequential := cypher.NewCanonicalNodeWriter(phaseHookExecutor{
				inner: live.exec, phase: phase, hook: hook, once: &sync.Once{},
			}, 500, nil)
			live.write(ctx, t, sequential, live.materialization(retryFixtureRepo, "gen-1", false))
			if phase == "after" {
				hook()
			}

			if got := live.relationshipTypeCounts(ctx, t, live.repoID(retryFixtureRepo.name)); !reflect.DeepEqual(got, wantReducerEdgesOnRepo) {
				t.Fatalf("workload_materialization before %s: reducer edges = %v, want the serial result %v", phase, got, wantReducerEdgesOnRepo)
			}
			live.assertProjectorEdges(ctx, t, retryFixtureRepo, "gen-1")
		})
	}
}

// TestLiveRepositoryZombieAttemptKeepsReducerEdges is matrix row 3: attempt N
// of a generation (its lease expired) writes after attempt N+1 succeeded and
// workload_materialization ran, first strictly after, then racing the reducer
// writes. The heartbeat and claim fences that cancel such a zombie live in
// Postgres and are unchanged; this proves that if its graph write lands
// anyway, it can no longer destroy other writers' edges.
func TestLiveRepositoryZombieAttemptKeepsReducerEdges(t *testing.T) {
	live := openRepoRetryLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	writer := live.writerShapes()["atomic_group"]
	repoID := live.repoID(retryFixtureRepo.name)

	for trial := 0; trial < 5; trial++ {
		live.cleanup(t)
		live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-1", true))
		live.write(ctx, t, writer, live.materialization(retryFixtureOther, "gen-1", true))
		live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-1", false)) // attempt N+1, acked
		live.materializeRetryWorkloads(ctx, t)
		live.seedReducerEdges(ctx, t)
		if trial == 0 {
			live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-1", false)) // zombie N, strictly after
		} else {
			start := make(chan struct{})
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				errs <- writer.Write(ctx, live.materialization(retryFixtureRepo, "gen-1", false))
			}()
			go func() {
				defer wg.Done()
				<-start
				if err := live.materializeRetryWorkloadsErr(ctx); err != nil {
					errs <- err
					return
				}
				errs <- live.seedReducerEdgesErr(ctx)
			}()
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatalf("trial %d zombie write: %v", trial, err)
				}
			}
		}
		if got := live.relationshipTypeCounts(ctx, t, repoID); !reflect.DeepEqual(got, wantReducerEdgesOnRepo) {
			t.Fatalf("trial %d: reducer edges after zombie attempt = %v, want %v", trial, got, wantReducerEdgesOnRepo)
		}
		live.assertProjectorEdges(ctx, t, retryFixtureRepo, "gen-1")
	}
}

// TestLiveRepositoryFirstCreationMergeRaceKeepsOneNode is matrix row 5: 50
// concurrent first creations of one Repository id, half through the real
// projector upsert and half through the real reducer stub MERGE
// (cypher.CanonicalRepoDependencyUpsertCypher). The schema's repository_id
// uniqueness constraint must yield exactly one node, and the projector's SET
// must own the final evidence_source.
func TestLiveRepositoryFirstCreationMergeRaceKeepsOneNode(t *testing.T) {
	live := openRepoRetryLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	writer := live.writerShapes()["atomic_group"]
	live.write(ctx, t, writer, live.materialization(retryFixtureOther, "gen-1", true))

	const sessions = 50
	for trial := 0; trial < 10; trial++ {
		fixture := repoFixture{name: fmt.Sprintf("race-%d", trial)}
		repoID := live.repoID(fixture.name)
		mat := live.materialization(fixture, "gen-1", true)
		start := make(chan struct{})
		errs := make(chan error, sessions)
		var wg sync.WaitGroup
		for session := 0; session < sessions; session++ {
			wg.Add(1)
			go func(session int) {
				defer wg.Done()
				<-start
				if session%2 == 0 {
					errs <- writer.Write(ctx, mat)
					return
				}
				// A managed transaction, like the production RetryingExecutor,
				// retries the transient DeadlockDetected two stubs locking both
				// Repository endpoints in opposite order can raise; the
				// invariant is one node, not zero transient retries.
				errs <- live.exec.ExecuteGroup(ctx, []cypher.Statement{{
					Cypher: cypher.CanonicalRepoDependencyUpsertCypher,
					Parameters: map[string]any{
						"repo_id": repoID, "target_repo_id": live.repoID(retryFixtureOther.name),
						"evidence_source": "resolver/cross-repo", "generation_id": "reducer-gen",
						"confidence": 0.9, "evidence_type": "test", "resolved_id": "resolved-" + repoID,
						"evidence_count": 1, "evidence_kinds": []string{"test"}, "resolution_source": "test",
						"rationale": "#7285 race", "source_tool": "test",
					},
				}})
			}(session)
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("trial %d concurrent MERGE: %v", trial, err)
			}
		}
		if got := live.count(ctx, t, `MATCH (r:Repository {id: $repo_id}) RETURN count(r) AS count`,
			map[string]any{"repo_id": repoID}); got != 1 {
			t.Fatalf("trial %d: Repository nodes with id %s = %d, want exactly 1", trial, repoID, got)
		}
		if got := live.repositoryProperties(ctx, t, repoID)["evidence_source"]; got != "projector/canonical" {
			t.Fatalf("trial %d: evidence_source = %v, want projector/canonical", trial, got)
		}
	}
}

// TestLiveRepositoryEdgeRetractRacesOtherScopeSameNamedWorkload is matrix row
// 6: a B1 zero-candidate retract of one repository races another scope's
// workload_materialization writing DEFINES for a workload of the same name.
// Workload ids are repository-namespaced and the retract anchors on its own
// Repository id, so only the retracting repository's edges may go.
func TestLiveRepositoryEdgeRetractRacesOtherScopeSameNamedWorkload(t *testing.T) {
	live := openRepoRetryLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	writer := live.writerShapes()["atomic_group"]
	live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-1", true))
	live.write(ctx, t, writer, live.materialization(retryFixtureOther, "gen-1", true))
	repo, other := retryFixtureRepo.name, retryFixtureOther.name

	for trial := 0; trial < 10; trial++ {
		gen := fmt.Sprintf("gen-%d", trial)
		live.handleWorkloads(ctx, t, repo, gen+"a", false, "api")
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := live.handleWorkloadsErr(ctx, repo, gen+"b", false)
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := live.handleWorkloadsErr(ctx, other, gen+"b", false, "api")
			errs <- err
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("trial %d: %v", trial, err)
			}
		}
		label := fmt.Sprintf("trial %d", trial)
		assertTargets(t, label+" retracting repository DEFINES", live.ownEdgeTargets(ctx, t, repo, "DEFINES", "finalization/workloads"))
		assertTargets(t, label+" other scope DEFINES", live.ownEdgeTargets(ctx, t, other, "DEFINES", "finalization/workloads"), "api")
		assertTargets(t, label+" other scope EXPOSES_ENDPOINT", live.ownEdgeTargets(ctx, t, other, "EXPOSES_ENDPOINT", "finalization/workloads"), "/v1/api")
	}
}
