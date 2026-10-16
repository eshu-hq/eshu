// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	gg "github.com/eshu-hq/eshu/go/internal/goldengate"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// deltaUnionRepoFilesCypher reads one repository's GRAPH side: its file set
// with per-path Function counts. The Repository anchor uses id (the
// canonical node key); the shape mirrors the production deadcode surface so
// it stays valid on both supported backends.
const deltaUnionRepoFilesCypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
OPTIONAL MATCH (f)-[:CONTAINS]->(e:Function)
RETURN f.relative_path AS rel, count(e) AS entities`

// deltaUnionGraphFiles is the per-repo graph read the delta-union phase
// needs. boltGraphCounter implements it against the live backend; tests fake
// it.
type deltaUnionGraphFiles interface {
	DeltaUnionRepoFiles(ctx context.Context, repoID string) (map[string]int64, error)
}

// DeltaUnionRepoFiles implements deltaUnionGraphFiles.
func (b *boltGraphCounter) DeltaUnionRepoFiles(ctx context.Context, repoID string) (map[string]int64, error) {
	result, err := neo4j.ExecuteQuery(ctx, b.driver, deltaUnionRepoFilesCypher,
		map[string]any{"repo_id": repoID},
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return nil, fmt.Errorf("execute delta-union repo files query: %w", err)
	}
	out := make(map[string]int64, len(result.Records))
	for _, record := range result.Records {
		rel, _ := record.Get("rel")
		path, _ := rel.(string)
		if path == "" {
			continue
		}
		var count int64
		if raw, ok := record.Get("entities"); ok {
			switch c := raw.(type) {
			case int64:
				count = c
			case int:
				count = int64(c)
			case float64:
				count = int64(c)
			}
		}
		out[path] = count
	}
	return out, nil
}

// runDeltaUnion is the delta-union phase (#7797): for every scope with an
// active delta generation, compare the graph's file set against the ACTIVE
// set (base-full overlaid with the active delta, read through the
// independent SQL, never through the projection's union function) and
// hard-fail on any mismatch. Scopes with no active generation, or whose
// active generation is not a delta, skip with a reason; the current corpus
// carries no delta-active scopes, so the phase passes vacuously until a
// delta scenario lands, and then it guards the repair.
func runDeltaUnion(ctx context.Context, getenv func(string) string, r *Report) error {
	pgdb, err := runtimecfg.OpenPostgres(ctx, getenv)
	if err != nil {
		return fmt.Errorf("delta-union phase: open postgres: %w", err)
	}
	defer func() { _ = pgdb.Close() }()
	store := pgstatus.NewFactStore(pgstatus.SQLDB{DB: pgdb})
	snapshot, err := store.ReadDeltaUnionActive(ctx, nil)
	if err != nil {
		return fmt.Errorf("delta-union phase: read active: %w", err)
	}
	counter, closeFn, err := openGraphCounter(ctx, getenv)
	if err != nil {
		return fmt.Errorf("delta-union phase: open graph: %w", err)
	}
	defer closeFn()
	return evaluateDeltaUnionSnapshot(ctx, snapshot, counter, r)
}

// evaluateDeltaUnionSnapshot assembles expectations from the ACTIVE snapshot,
// reads each repository's GRAPH side, and evaluates. It is the unit-testable
// core; runDeltaUnion only opens the backends.
func evaluateDeltaUnionSnapshot(
	ctx context.Context,
	snapshot pgstatus.DeltaUnionActiveSnapshot,
	graph deltaUnionGraphFiles,
	r *Report,
) error {
	expected := gg.AssembleDeltaUnionExpectations(snapshot.Rows, snapshot.Deltas)
	observations := make([]gg.DeltaUnionObservation, 0, len(expected))
	for _, exp := range expected {
		observation := gg.DeltaUnionObservation{RepoID: exp.RepoID, Files: map[string]int64{}}
		// Only delta-active scopes are compared: the check covers
		// delta-active scopes, so a full active generation is out of
		// this comparison's scope and skips without a graph read.
		if exp.ActiveGenerationID != "" && exp.ActiveIsDelta && exp.RepoID != "" {
			files, err := graph.DeltaUnionRepoFiles(ctx, exp.RepoID)
			if err != nil {
				return fmt.Errorf("delta-union phase: read graph for repo %s: %w", exp.RepoID, err)
			}
			observation.Files = files
		}
		observations = append(observations, observation)
	}
	gg.EvaluateDeltaUnionCompleteness(expected, observations, r)
	return nil
}
