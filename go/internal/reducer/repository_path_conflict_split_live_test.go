// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package reducer_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestLiveRepositoryPathConflictEdgelessRetirementReportsCleanSplit is the
// #7324 negative case: a different-id Repository holding the path but
// carrying no edge retires clean at INFO with an attributed empty split
// (attribution=true, zero direction totals) while only the outcome counter
// moves. The old node is created bare on purpose: the fixture
// materialization always projects files and directories, so only a
// hand-built node is truly edgeless. Runs under both production executor
// shapes; pinned backends:neo4j with the retirement proof file.
func TestLiveRepositoryPathConflictEdgelessRetirementReportsCleanSplit(t *testing.T) {
	live := openRepoRetryLive(t)
	for _, shape := range []string{"atomic_group", "phase_group"} {
		t.Run(shape, func(t *testing.T) {
			live.cleanup(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			writer := live.instrumentedWriterShapes(instruments)[shape]
			oldID := live.repoID("edgeless-old")
			newID := live.repoID("edgeless-new")
			path := live.repoPath("edgeless")
			live.run(ctx, t, `MERGE (r:Repository {id: $old}) SET r.path = $path`,
				map[string]any{"old": oldID, "path": path})
			if got := live.count(ctx, t, `MATCH (:Repository {id: $old})-[rel]-() RETURN count(rel) AS count`,
				map[string]any{"old": oldID}); got != 0 {
				t.Fatalf("negative control: edges on the old-id Repository = %d, want 0", got)
			}

			mat := live.materialization(retryFixtureOther, "gen-2", false)
			mat.RepoID = newID
			mat.ScopeID = "git-repository-scope:" + newID
			mat.Repository.RepoID = newID
			mat.Repository.Path = path
			logs := captureJSONLogs(t)
			live.write(ctx, t, writer, mat)

			if got := live.count(ctx, t, `MATCH (r:Repository {id: $old}) RETURN count(r) AS count`,
				map[string]any{"old": oldID}); got != 0 {
				t.Fatalf("old-id Repository nodes after the path-conflict projection = %d, want 0 (retired)", got)
			}
			var retired []map[string]any
			for _, entry := range logs() {
				if entry["msg"] == "canonical repository retired" {
					retired = append(retired, entry)
				}
			}
			if len(retired) != 1 {
				t.Fatalf("`canonical repository retired` log lines = %d, want 1; logs = %v", len(retired), logs())
			}
			entry := retired[0]
			for key, want := range map[string]any{
				"level": "INFO", "repo_id": newID, "path": path,
				"deletes_counted": true, "attribution": true,
				"nodes_deleted": float64(1), "relationships_deleted": float64(0),
				"dropped_incoming": float64(0), "dropped_outgoing": float64(0),
				"outcome": "clean",
			} {
				if entry[key] != want {
					t.Fatalf("retirement log %s = %#v, want %#v; entry = %v", key, entry[key], want, entry)
				}
			}
			const metricName = "eshu_dp_canonical_repository_retirements_total"
			if got := counterValue(t, reader, metricName, "outcome", "clean"); got != 1 {
				t.Fatalf("%s{outcome=clean} = %d, want 1", metricName, got)
			}
			const splitMetric = "eshu_dp_canonical_repository_retired_relationships_total"
			for _, bucket := range [][2]string{
				{"incoming", "reducer"},
				{"incoming", "projector"},
				{"incoming", "other"},
				{"outgoing", "reducer"},
				{"outgoing", "projector"},
				{"outgoing", "other"},
			} {
				if got := counterValue2(t, reader, splitMetric, "direction", bucket[0], "class", bucket[1]); got != 0 {
					t.Fatalf("%s{direction=%s,class=%s} = %d, want 0 for an edgeless retirement",
						splitMetric, bucket[0], bucket[1], got)
				}
			}
		})
	}
	// #7445: the shared cleanup must remove the path-less stub the writers
	// re-created under the retired id; it carries no path and no uid, so
	// only its id ties it to this test's prefix.
	live.cleanup(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if got := live.count(ctx, t, `MATCH (r:Repository) WHERE r.id STARTS WITH 'repository:' + $prefix RETURN count(r) AS count`,
		map[string]any{"prefix": live.prefix}); got != 0 {
		t.Fatalf("prefix-scoped Repository nodes after cleanup = %d, want 0", got)
	}
}

// TestLiveRepositoryPathConflictConcurrentWritersBoundsSplit is the #7324
// ambiguous case: writers keep MERGE-ing edges into the old-id Repository
// while the retirement write runs. Which concurrent edges the retirement
// sees is interleaving, so no path pins the exact composition -- but the
// floor holds on every path (the 11 seeded edges were all committed before
// the race, so dropped_incoming >= 11 and relationships_deleted >= 20).
// The sum is floor-only too, on every path: a racing edge can commit
// between the statement's pre-delete collection and its delete (Neo4j
// read-committed), so the collection can legitimately undercount the
// delete. Measured on Neo4j: atomic 13 + 9 collected vs 24 deleted,
// phase-group 14 + 9 collected vs 28 deleted. Sum-exactness is a quiescence
// property, pinned by the positive scenario on both shapes; under hammering
// every shape is floor-only. Late writers may re-create a stub under the
// retired id after the delete, so the old id is not asserted gone here.
// Runs under both production executor shapes; pinned backends:neo4j with
// the retirement proof file.
func TestLiveRepositoryPathConflictConcurrentWritersBoundsSplit(t *testing.T) {
	live := openRepoRetryLive(t)
	for _, shape := range []string{"atomic_group", "phase_group"} {
		t.Run(shape, func(t *testing.T) {
			live.cleanup(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			writer := live.instrumentedWriterShapes(instruments)[shape]
			newID, oldID := live.repoID(retryFixtureRepo.name), live.repoID(retryFixtureRepo.name+"-rekeyed")
			path := live.repoPath(retryFixtureRepo.name)

			live.write(ctx, t, writer, live.materialization(retryFixtureOther, "gen-1", true))
			live.write(ctx, t, writer, live.materializationAs(retryFixtureRepo, oldID, "gen-1", true))
			params := map[string]any{"old": oldID, "other": live.repoID(retryFixtureOther.name), "prefix": live.prefix}
			for _, seed := range retiredIncomingSeeds {
				live.run(ctx, t, seed, params)
			}

			// Four writers race the retirement, five distinct-type edges
			// each. A writer that loses a deadlock only shrinks the
			// unasserted surplus: the floor below rests on the committed
			// seeds, so errors are logged, not failed.
			const raceWriters, raceEdges = 4, 5
			raceDone := make(chan error, raceWriters)
			for g := 0; g < raceWriters; g++ {
				go func(g int) {
					var firstErr error
					for k := 0; k < raceEdges; k++ {
						// Relationship types cannot be parameterized, so the
						// distinct race type is concatenated (test-only).
						edgeType := fmt.Sprintf("RACE_EDGE_%d_%d", g, k)
						raceErr := live.exec.Execute(ctx, cypher.Statement{
							Cypher: `MATCH (r:Repository {id: $old}), (o:Repository {id: $other}) MERGE (o)-[x:` + edgeType + `]->(r) SET x.evidence_source = 'test/7324-race'`,
							Parameters: map[string]any{
								"old": oldID, "other": live.repoID(retryFixtureOther.name),
							},
						})
						if raceErr != nil && firstErr == nil {
							firstErr = raceErr
						}
					}
					raceDone <- firstErr
				}(g)
			}
			logs := captureJSONLogs(t)
			live.write(ctx, t, writer, live.materialization(retryFixtureRepo, "gen-2", false))
			for g := 0; g < raceWriters; g++ {
				if raceErr := <-raceDone; raceErr != nil {
					t.Logf("race writer %d error (surplus only, floor unaffected): %v", g, raceErr)
				}
			}

			pathParams := map[string]any{"path": path, "new": newID}
			if got := live.count(ctx, t, `MATCH (r:Repository {path: $path}) WHERE r.id = $new RETURN count(r) AS count`, pathParams); got != 1 {
				t.Fatalf("new-id Repository at the path = %d, want 1", got)
			}
			var retired []map[string]any
			for _, entry := range logs() {
				if entry["msg"] == "canonical repository retired" {
					retired = append(retired, entry)
				}
			}
			if len(retired) != 1 {
				t.Fatalf("`canonical repository retired` log lines = %d, want 1; logs = %v", len(retired), logs())
			}
			entry := retired[0]
			relationshipsDeleted, _ := entry["relationships_deleted"].(float64)
			droppedIncoming, _ := entry["dropped_incoming"].(float64)
			droppedOutgoing, _ := entry["dropped_outgoing"].(float64)
			if relationshipsDeleted < 20 {
				t.Fatalf("relationships_deleted = %v, want >= 20 (11 seeds + 9 projector edges); entry = %v",
					entry["relationships_deleted"], entry)
			}
			if droppedIncoming < 11 {
				t.Fatalf("dropped_incoming = %v, want >= 11 (the committed seeds); entry = %v",
					entry["dropped_incoming"], entry)
			}
			// No sum assertion: measured runs show the collection
			// undercounting the delete on every shape when edges commit
			// mid-statement, so only the floors above are deterministic.
			t.Logf("concurrent retirement: dropped_incoming=%v dropped_outgoing=%v relationships_deleted=%v",
				droppedIncoming, droppedOutgoing, relationshipsDeleted)
		})
	}
	// #7445: the shared cleanup must remove the path-less stub the writers
	// re-created under the retired id; it carries no path and no uid, so
	// only its id ties it to this test's prefix.
	live.cleanup(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if got := live.count(ctx, t, `MATCH (r:Repository) WHERE r.id STARTS WITH 'repository:' + $prefix RETURN count(r) AS count`,
		map[string]any{"prefix": live.prefix}); got != 0 {
		t.Fatalf("prefix-scoped Repository nodes after cleanup = %d, want 0", got)
	}
}
