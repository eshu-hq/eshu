// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	gg "github.com/eshu-hq/eshu/go/internal/goldengate"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// deltaReaderRepoEdgesCypher reads every relationship leaving an entity of one
// repository. It anchors on the Repository id through the same
// REPO_CONTAINS/CONTAINS path the delta-union file read uses, so it stays
// valid on both supported backends; the per-assertion match runs in Go
// (gg.EvaluateDeltaReaderAssertions) over the small fixture repository.
const deltaReaderRepoEdgesCypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(a)-[rel]->(b)
RETURN labels(a) AS from_labels, a.name AS from_name, type(rel) AS rel_type,
       labels(b) AS to_labels, b.name AS to_name`

// deltaReaderActiveGenerationSQL reads the scope's active generation and
// whether it is a delta. A scope with no row, or no active generation,
// returns "" so the evaluator fails the stage check instead of passing
// vacuously.
const deltaReaderActiveGenerationSQL = `SELECT COALESCE(scope.active_generation_id, ''),
       COALESCE(generation.is_delta, false)
FROM ingestion_scopes AS scope
LEFT JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
WHERE scope.scope_id = $1`

// deltaReaderGraph is the repository edge read the delta-readers phase
// needs. boltGraphCounter implements it against the live backend; tests fake
// it.
type deltaReaderGraph interface {
	DeltaReaderRepoEdges(ctx context.Context, repoID string) ([]gg.DeltaReaderObservedEdge, error)
}

// deltaReaderGenerations reads a scope's active generation state.
type deltaReaderGenerations interface {
	ActiveGeneration(ctx context.Context, scopeID string) (generationID string, isDelta bool, err error)
}

// DeltaReaderRepoEdges implements deltaReaderGraph.
func (b *boltGraphCounter) DeltaReaderRepoEdges(ctx context.Context, repoID string) ([]gg.DeltaReaderObservedEdge, error) {
	result, err := neo4j.ExecuteQuery(ctx, b.driver, deltaReaderRepoEdgesCypher,
		map[string]any{"repo_id": repoID},
		neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
	if err != nil {
		return nil, fmt.Errorf("execute delta-reader repo edges query: %w", err)
	}
	out := make([]gg.DeltaReaderObservedEdge, 0, len(result.Records))
	for _, record := range result.Records {
		out = append(out, gg.DeltaReaderObservedEdge{
			Relationship: recordString(record, "rel_type"),
			FromLabels:   recordStrings(record, "from_labels"),
			FromName:     recordString(record, "from_name"),
			ToLabels:     recordStrings(record, "to_labels"),
			ToName:       recordString(record, "to_name"),
		})
	}
	return out, nil
}

func recordString(record *neo4j.Record, key string) string {
	raw, _ := record.Get(key)
	value, _ := raw.(string)
	return value
}

func recordStrings(record *neo4j.Record, key string) []string {
	raw, _ := record.Get(key)
	items, _ := raw.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

// sqlGenerations implements deltaReaderGenerations over Postgres.
type sqlGenerations struct{ db *sql.DB }

// ActiveGeneration implements deltaReaderGenerations.
func (s sqlGenerations) ActiveGeneration(ctx context.Context, scopeID string) (string, bool, error) {
	var (
		generationID string
		isDelta      bool
	)
	err := s.db.QueryRowContext(ctx, deltaReaderActiveGenerationSQL, scopeID).Scan(&generationID, &isDelta)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read active generation for scope %s: %w", scopeID, err)
	}
	return generationID, isDelta, nil
}

// runDeltaReaders is the opt-in delta-readers phase (#10022): it reads the
// delta fixture scope's generation state and its repository's entity edges,
// then asserts the snapshot's graph.delta_reader_assertions for the stage.
func runDeltaReaders(ctx context.Context, o options, getenv func(string) string, snap Snapshot, r *Report) error {
	pgdb, err := runtimecfg.OpenPostgresForWorkload(ctx, getenv, runtimecfg.PostgresWorkloadRuntime)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() { _ = pgdb.Close() }()
	counter, closeFn, err := openGraphCounter(ctx, getenv)
	if err != nil {
		return fmt.Errorf("open graph: %w", err)
	}
	defer closeFn()
	return evaluateDeltaReaders(ctx, snap.Graph.DeltaReaderAssertions,
		gg.DeltaReaderStage(o.deltaReaderStage), !o.deltaReaderIgnoreExpectedFailures,
		sqlGenerations{db: pgdb}, counter, r)
}

// evaluateDeltaReaders is the unit-testable core of runDeltaReaders. An
// absent or invalid section is left to the evaluator, which fails it, so the
// phase never reads a backend for a spec it cannot assert.
func evaluateDeltaReaders(
	ctx context.Context,
	spec *gg.DeltaReaderAssertions,
	stage gg.DeltaReaderStage,
	honorExpectedFailures bool,
	generations deltaReaderGenerations,
	graph deltaReaderGraph,
	r *Report,
) error {
	var obs gg.DeltaReaderObservation
	if spec != nil && spec.ScopeID != "" && spec.RepositoryID != "" {
		generationID, isDelta, err := generations.ActiveGeneration(ctx, spec.ScopeID)
		if err != nil {
			return err
		}
		obs.ActiveGenerationID, obs.ActiveIsDelta = generationID, isDelta
		edges, err := graph.DeltaReaderRepoEdges(ctx, spec.RepositoryID)
		if err != nil {
			return fmt.Errorf("read graph for repo %s: %w", spec.RepositoryID, err)
		}
		obs.Edges = edges
	}
	gg.EvaluateDeltaReaderAssertions(spec, stage, obs, honorExpectedFailures, r)
	return nil
}
