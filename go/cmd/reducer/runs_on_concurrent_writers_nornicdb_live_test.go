// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// runsOnBarrierDefaultTrials is the #6671 floor: at least 50 fresh pairs, each
// raced once by both production writers.
const runsOnBarrierDefaultTrials = 60

// runsOnRaceModes are the two arms every RUNS_ON race test runs: both writers
// released from one barrier, and the same writers one after the other.
var runsOnRaceModes = []string{"concurrent_barrier", "sequential_control"}

// TestRunsOnConcurrentWritersOneEdgePerPairLive is the #6671 multiplicity
// proof. It races the two production RUNS_ON writers on fresh
// (WorkloadInstance, Platform) pairs and asserts each pair ends with exactly
// one edge that carries the shared canonical identity and a non-empty
// evidence_source.
//
// Workload materialization (WorkloadMaterializer, serialized per scope by the
// platform_graph claim) and the repo_dependency lane (EdgeWriter, serialized by
// its partition lease) have different serializers, so nothing in Eshu stops
// them overlapping on one pair. Both MERGE
// `[:RUNS_ON {identity_key: 'canonical'}]`. An empty evidence_source would mean
// a writer's MERGE committed without its property statement, the other
// symptom #6671 recorded.
//
// Which writer's tuple survives is a separate contract with a known open
// defect; TestRunsOnConcurrentWritersCrossRepoTupleWinsLive owns it, so this
// test stays a clean multiplicity guard.
func TestRunsOnConcurrentWritersOneEdgePerPairLive(t *testing.T) {
	runner, trials := openRunsOnRaceBackend(t)
	for _, mode := range runsOnRaceModes {
		t.Run(mode, func(t *testing.T) {
			results := raceRunsOnWriters(t, runner, runner, mode, trials)
			var violations []string
			for index, rows := range results {
				switch {
				case len(rows) != 1:
					violations = append(violations, fmt.Sprintf("trial %d: %d RUNS_ON edges %v", index, len(rows), rows))
				case rows[0]["identity_key"] != "canonical":
					violations = append(violations, fmt.Sprintf("trial %d: identity_key %v, want canonical", index, rows[0]["identity_key"]))
				case rows[0]["evidence_source"] == nil || rows[0]["evidence_source"] == "":
					violations = append(violations, fmt.Sprintf("trial %d: RUNS_ON edge committed with no evidence_source %v", index, rows[0]))
				}
			}
			t.Logf("%s: trials=%d multiplicity_violations=%d", mode, len(results), len(violations))
			if len(violations) > 0 {
				t.Fatalf("%s: %d of %d pairs do not hold exactly one stamped canonical RUNS_ON edge:\n%s",
					mode, len(violations), len(results), strings.Join(violations, "\n"))
			}
		})
	}
}

// TestRunsOnConcurrentWritersCrossRepoTupleWinsLive asserts the #6634 tuple
// contract under the same race: the repo_dependency writer's full-tuple SET
// wins when it commits second, and the workload writer's ownership predicate
// preserves it when the workload writer commits second, so every pair must end
// with the cross-repo tuple.
//
// This test is known to fail intermittently on the pinned NornicDB: a rare
// barrier trial ends with the workload tuple (#FOLLOWUP-6671, see
// docs/internal/evidence/6671-runs-on-concurrent-writers.md). It hard-fails on
// purpose; a lost cross-repo tuple is not reaped by RetractRepoRunsOnEdgesCypher,
// so it is an accuracy defect, not noise to log.
func TestRunsOnConcurrentWritersCrossRepoTupleWinsLive(t *testing.T) {
	runner, trials := openRunsOnRaceBackend(t)
	for _, mode := range runsOnRaceModes {
		t.Run(mode, func(t *testing.T) {
			results := raceRunsOnWriters(t, runner, runner, mode, trials)
			var violations []string
			for index, rows := range results {
				if len(rows) != 1 {
					violations = append(violations, fmt.Sprintf("trial %d: %d RUNS_ON edges %v", index, len(rows), rows))
					continue
				}
				row := rows[0]
				if row["evidence_source"] != reducer.CrossRepoEvidenceSource || row["confidence"] != 0.97 || row["source_tool"] != "argocd" {
					violations = append(violations, fmt.Sprintf("trial %d: RUNS_ON tuple %v, want the cross-repo tuple", index, row))
				}
			}
			t.Logf("%s: trials=%d tuple_violations=%d", mode, len(results), len(violations))
			if len(violations) > 0 {
				t.Fatalf("%s: %d of %d pairs lost the cross-repo RUNS_ON tuple:\n%s",
					mode, len(violations), len(results), strings.Join(violations, "\n"))
			}
		})
	}
}

// openRunsOnRaceBackend opens the Bolt backend named by ESHU_CYPHER_BOLT_DSN
// and resolves the trial count. ESHU_RUNS_ON_BARRIER_TRIALS raises the count for
// soak runs; it cannot lower it below the #6671 floor.
func openRunsOnRaceBackend(t *testing.T) (neo4jSessionRunner, int) {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if uri == "" {
		t.Skip("ESHU_CYPHER_BOLT_DSN not set; skipping isolated Bolt graph test")
	}
	trials := runsOnBarrierDefaultTrials
	if raw := strings.TrimSpace(os.Getenv("ESHU_RUNS_ON_BARRIER_TRIALS")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < runsOnBarrierDefaultTrials {
			t.Fatalf("ESHU_RUNS_ON_BARRIER_TRIALS=%q, want an integer >= %d", raw, runsOnBarrierDefaultTrials)
		}
		trials = parsed
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify Bolt connectivity: %v", err)
	}
	return neo4jSessionRunner{Driver: driver, DatabaseName: "nornic", TxTimeout: 30 * time.Second}, trials
}

// raceRunsOnWriters seeds trials fresh pairs, drives both production writers
// on each (from one barrier, or one after the other alternating the order),
// and returns every pair's RUNS_ON rows. writers is the cypherRunner the
// production adapters wrap; reads and seeding go straight to runner.
//
// Shape: one RuntimePlatformRow and one intent row per writer call, EdgeWriter
// batch size 1. Production batches repo_dependency rows, so this proves the
// single-pair race, not a multi-row batch colliding with another batch.
func raceRunsOnWriters(t *testing.T, runner neo4jSessionRunner, writers cypherRunner, mode string, trials int) [][]map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pairs := seedRunsOnRacePairs(t, ctx, runner, mode, trials)
	materializer := reducer.NewWorkloadMaterializer(newReducerCypherExecutor(writers, nil, nil))
	edgeWriter := edgewriter.NewEdgeWriter(newReducerNeo4jExecutor(writers, nil), 1)
	started := time.Now()
	var slowest time.Duration
	for index, pair := range pairs {
		writeWorkload := func() error {
			_, err := materializer.Materialize(ctx, &reducer.ProjectionResult{
				RuntimePlatformRows: []reducer.RuntimePlatformRow{{
					Confidence:   0.42,
					InstanceID:   pair.instance,
					PlatformID:   pair.platform,
					PlatformKind: "kubernetes",
					PlatformName: "runs-on-race",
					RepoID:       pair.repo,
				}},
			})
			return err
		}
		writeCrossRepo := func() error {
			_, err := edgeWriter.WriteEdges(ctx, reducer.DomainRepoDependency, []reducer.SharedProjectionIntentRow{{
				IntentID:     pair.repo + "-runs-on",
				RepositoryID: pair.repo,
				Payload: map[string]any{
					"repo_id":           pair.repo,
					"platform_id":       pair.platform,
					"relationship_type": "RUNS_ON",
					"source_tool":       "argocd",
				},
			}}, reducer.CrossRepoEvidenceSource)
			return err
		}
		trialStarted := time.Now()
		var workloadErr, crossRepoErr error
		switch {
		case mode == "concurrent_barrier":
			workloadErr, crossRepoErr = runRunsOnWritersAtBarrier(writeWorkload, writeCrossRepo)
		case index%2 == 0:
			workloadErr = writeWorkload()
			crossRepoErr = writeCrossRepo()
		default:
			crossRepoErr = writeCrossRepo()
			workloadErr = writeWorkload()
		}
		if workloadErr != nil || crossRepoErr != nil {
			t.Fatalf("trial %d: workload writer err=%v, cross-repo writer err=%v", index, workloadErr, crossRepoErr)
		}
		if took := time.Since(trialStarted); took > slowest {
			slowest = took
		}
	}
	// A barrier trial that collides costs one driver-level managed transaction
	// retry (Neo.TransientError.Transaction.Outdated on the shared edge), so
	// slowest_trial shows the retry backoff an overlap pays.
	t.Logf("%s: trials=%d elapsed=%s slowest_trial=%s", mode, len(pairs), time.Since(started), slowest)

	results := make([][]map[string]any, len(pairs))
	for index, pair := range pairs {
		rows, err := runner.Run(ctx, `
MATCH (:WorkloadInstance {id: $instance_id})-[rel:RUNS_ON]->(:Platform {id: $platform_id})
RETURN rel.identity_key AS identity_key, rel.evidence_source AS evidence_source,
       rel.confidence AS confidence, rel.source_tool AS source_tool`, map[string]any{
			"instance_id": pair.instance, "platform_id": pair.platform,
		})
		if err != nil {
			t.Fatalf("trial %d: read RUNS_ON pair: %v", index, err)
		}
		results[index] = rows
	}
	return results
}

// runRunsOnWritersAtBarrier starts both writers, parks them on one barrier,
// and releases them together so their managed transactions overlap.
func runRunsOnWritersAtBarrier(writeWorkload, writeCrossRepo func() error) (error, error) {
	var ready, done sync.WaitGroup
	release := make(chan struct{})
	var workloadErr, crossRepoErr error
	ready.Add(2)
	done.Add(2)
	go func() {
		defer done.Done()
		ready.Done()
		<-release
		workloadErr = writeWorkload()
	}()
	go func() {
		defer done.Done()
		ready.Done()
		<-release
		crossRepoErr = writeCrossRepo()
	}()
	ready.Wait()
	close(release)
	done.Wait()
	return workloadErr, crossRepoErr
}

type runsOnRacePair struct {
	repo, workload, instance, platform string
}

// seedRunsOnRacePairs creates count fresh Repository -> Workload <-
// WorkloadInstance chains, each with its own Platform and no RUNS_ON edge, so
// every trial starts from the create path both writers race on.
func seedRunsOnRacePairs(t *testing.T, ctx context.Context, runner neo4jSessionRunner, mode string, count int) []runsOnRacePair {
	t.Helper()
	prefix := fmt.Sprintf("i6671-%s-%d", mode, time.Now().UnixNano())
	pairs := make([]runsOnRacePair, count)
	rows := make([]map[string]any, count)
	ids := make([]string, 0, 4*count)
	for index := range pairs {
		base := fmt.Sprintf("%s-%03d", prefix, index)
		pairs[index] = runsOnRacePair{
			repo: base + "-repo", workload: base + "-workload",
			instance: base + "-instance", platform: base + "-platform",
		}
		rows[index] = map[string]any{
			"repo_id": pairs[index].repo, "workload_id": pairs[index].workload,
			"instance_id": pairs[index].instance, "platform_id": pairs[index].platform,
		}
		ids = append(ids, pairs[index].repo, pairs[index].workload, pairs[index].instance, pairs[index].platform)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := runner.RunCypher(cleanupCtx, `MATCH (n) WHERE n.id IN $ids DETACH DELETE n`, map[string]any{"ids": ids}); err != nil {
			t.Errorf("clean RUNS_ON race pairs: %v", err)
		}
	})
	if err := runner.RunCypher(ctx, `UNWIND $rows AS row
CREATE (repo:Repository {id: row.repo_id})
CREATE (workload:Workload {id: row.workload_id})
CREATE (instance:WorkloadInstance {id: row.instance_id})
CREATE (platform:Platform {id: row.platform_id})`, map[string]any{"rows": rows}); err != nil {
		t.Fatalf("seed RUNS_ON race nodes: %v", err)
	}
	if err := runner.RunCypher(ctx, `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})
MATCH (workload:Workload {id: row.workload_id})
MATCH (instance:WorkloadInstance {id: row.instance_id})
MERGE (repo)-[:DEFINES]->(workload)
MERGE (instance)-[:INSTANCE_OF]->(workload)`, map[string]any{"rows": rows}); err != nil {
		t.Fatalf("seed RUNS_ON race chains: %v", err)
	}
	return pairs
}
