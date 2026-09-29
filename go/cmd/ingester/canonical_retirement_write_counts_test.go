// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// #7324 review F1. The ingester runs a projector in-process with its own
// canonical writer over ingesterNeo4jExecutor, so that executor must report
// Bolt write summaries through sourcecypher.ReportWriteCounts exactly like
// cmd/projector/neo4j_executor.go. Without it a real path-conflict Repository
// retirement in the ingester recorded no counter and no WARN.
//
// The fake driver below implements only the driver methods the executor
// calls; the embedded interfaces panic on anything else, so a new driver call
// fails loudly instead of passing silently.

// isPathCleanupCypher recognises the writer's path-conflict retirement
// statement from outside package cypher.
func isPathCleanupCypher(cypher string) bool {
	return strings.Contains(cypher, "MATCH (r:Repository {path: $path})") && strings.Contains(cypher, "DETACH DELETE r")
}

type summaryDriver struct {
	neo4jdriver.DriverWithContext
	cleanupNodes, cleanupRelationships int
}

func (d *summaryDriver) NewSession(context.Context, neo4jdriver.SessionConfig) neo4jdriver.SessionWithContext {
	return &summarySession{driver: d}
}

func (d *summaryDriver) result(cypher string) neo4jdriver.ResultWithContext {
	counters := summaryCounters{}
	if isPathCleanupCypher(cypher) {
		counters = summaryCounters{nodesDeleted: d.cleanupNodes, relationshipsDeleted: d.cleanupRelationships}
	}
	return &summaryResult{counters: counters}
}

type summarySession struct {
	neo4jdriver.SessionWithContext
	driver *summaryDriver
}

func (s *summarySession) Run(_ context.Context, cypher string, _ map[string]any, _ ...func(*neo4jdriver.TransactionConfig)) (neo4jdriver.ResultWithContext, error) {
	return s.driver.result(cypher), nil
}

func (s *summarySession) ExecuteWrite(_ context.Context, work neo4jdriver.ManagedTransactionWork, _ ...func(*neo4jdriver.TransactionConfig)) (any, error) {
	return work(&summaryTx{driver: s.driver})
}

func (s *summarySession) Close(context.Context) error { return nil }

type summaryTx struct {
	neo4jdriver.ManagedTransaction
	driver *summaryDriver
}

func (tx *summaryTx) Run(_ context.Context, cypher string, _ map[string]any) (neo4jdriver.ResultWithContext, error) {
	return tx.driver.result(cypher), nil
}

type summaryResult struct {
	neo4jdriver.ResultWithContext
	counters summaryCounters
}

func (r *summaryResult) Next(context.Context) bool { return false }
func (r *summaryResult) Err() error                { return nil }
func (r *summaryResult) Consume(context.Context) (neo4jdriver.ResultSummary, error) {
	return summaryOf{counters: r.counters}, nil
}

type summaryOf struct {
	neo4jdriver.ResultSummary
	counters summaryCounters
}

func (s summaryOf) Counters() neo4jdriver.Counters { return s.counters }

type summaryCounters struct {
	neo4jdriver.Counters
	nodesDeleted, relationshipsDeleted int
}

func (c summaryCounters) NodesCreated() int         { return 0 }
func (c summaryCounters) NodesDeleted() int         { return c.nodesDeleted }
func (c summaryCounters) RelationshipsCreated() int { return 0 }
func (c summaryCounters) RelationshipsDeleted() int { return c.relationshipsDeleted }
func (c summaryCounters) PropertiesSet() int        { return 0 }
func (c summaryCounters) LabelsAdded() int          { return 0 }
func (c summaryCounters) LabelsRemoved() int        { return 0 }

func TestIngesterNeo4jExecutorReportsWriteCountsOnEveryStatementPath(t *testing.T) {
	t.Parallel()

	cleanup := sourcecypher.Statement{
		Cypher:     "MATCH (r:Repository {path: $path})\nWHERE r.id <> $repo_id\nDETACH DELETE r",
		Parameters: map[string]any{"path": "/repos/service", "repo_id": "repository:r_new"},
	}
	cases := map[string]struct {
		profileFileGroups bool
		run               func(context.Context, ingesterNeo4jExecutor) error
		want              int
	}{
		"Execute": {run: func(ctx context.Context, e ingesterNeo4jExecutor) error {
			return e.Execute(ctx, cleanup)
		}, want: 1},
		"ExecuteGroup": {run: func(ctx context.Context, e ingesterNeo4jExecutor) error {
			return e.ExecuteGroup(ctx, []sourcecypher.Statement{cleanup, {Cypher: "RETURN 1"}})
		}, want: 2},
		"ExecuteGroup file-group probe": {profileFileGroups: true, run: func(ctx context.Context, e ingesterNeo4jExecutor) error {
			return e.ExecuteGroup(ctx, []sourcecypher.Statement{testFileProbeStatement(t)})
		}, want: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			collector := sourcecypher.NewWriteCountsCollector()
			ctx := sourcecypher.WithWriteCountsCollector(context.Background(), collector)
			exec := ingesterNeo4jExecutor{
				Driver:            &summaryDriver{cleanupNodes: 1, cleanupRelationships: 5},
				ProfileFileGroups: tc.profileFileGroups,
			}
			if err := tc.run(ctx, exec); err != nil {
				t.Fatalf("run: %v", err)
			}
			entries := collector.Entries()
			if len(entries) != tc.want {
				t.Fatalf("reported write-count entries = %d, want %d (one per executed statement)", len(entries), tc.want)
			}
			if isPathCleanupCypher(entries[0].Cypher) {
				if got := entries[0].Counters; got.NodesDeleted != 1 || got.RelationshipsDeleted != 5 {
					t.Fatalf("cleanup counters = %+v, want NodesDeleted=1 RelationshipsDeleted=5 from the Bolt summary", got)
				}
			}
		})
	}
}

// TestIngesterCanonicalWriterReportsPathConflictRetirement drives the real
// ingester executor chain (canonicalExecutorForGraphBackend over
// ingesterNeo4jExecutor) under the canonical writer on the retry shape, for
// both graph backends: the Neo4j atomic group (ExecuteGroup) and the NornicDB
// phase-group retract phase (Execute).
func TestIngesterCanonicalWriterReportsPathConflictRetirement(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	for _, backend := range []runtimecfg.GraphBackend{runtimecfg.GraphBackendNeo4j, runtimecfg.GraphBackendNornicDB} {
		t.Run(string(backend), func(t *testing.T) {
			logs.Reset()
			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			chain := canonicalExecutorForGraphBackend(
				ingesterNeo4jExecutor{Driver: &summaryDriver{cleanupNodes: 1, cleanupRelationships: 5}, Instruments: instruments},
				backend, 0, false,
				defaultNornicDBPhaseGroupStatements, defaultNornicDBFilePhaseStatements,
				defaultNornicDBStructuralEdgePhaseStatements, defaultNornicDBEntityPhaseStatements,
				nil, 1, 0, nil, instruments, nil,
			)
			err = sourcecypher.NewCanonicalNodeWriter(chain, 500, instruments).Write(context.Background(), canonical.CanonicalMaterialization{
				ScopeID: "scope-1", GenerationID: "gen-2", RepoID: "repository:r_new", RepoPath: "/repos/service",
				Repository: &canonical.RepositoryRow{RepoID: "repository:r_new", Name: "service", Path: "/repos/service"},
			})
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			var retired []map[string]any
			for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
				var entry map[string]any
				if json.Unmarshal(line, &entry) == nil && entry["msg"] == "canonical repository retired" {
					retired = append(retired, entry)
				}
			}
			if len(retired) != 1 {
				t.Fatalf("`canonical repository retired` lines = %d, want 1; logs = %s", len(retired), logs.String())
			}
			for key, want := range map[string]any{
				"level": "WARN", "nodes_deleted": float64(1), "relationships_deleted": float64(5), "deletes_counted": true,
			} {
				if retired[0][key] != want {
					t.Fatalf("retirement log %s = %#v, want %#v; entry = %v", key, retired[0][key], want, retired[0])
				}
			}
			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("collect: %v", err)
			}
			var dropped int64
			for _, scope := range rm.ScopeMetrics {
				for _, m := range scope.Metrics {
					if m.Name != "eshu_dp_canonical_repository_retirements_total" {
						continue
					}
					for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
						if v, ok := point.Attributes.Value(telemetry.MetricDimensionOutcome); ok &&
							v.AsString() == telemetry.RepositoryRetirementOutcomeDroppedRelationships {
							dropped += point.Value
						}
					}
				}
			}
			if dropped != 1 {
				t.Fatalf("eshu_dp_canonical_repository_retirements_total{outcome=dropped_relationships} = %d, want 1", dropped)
			}
		})
	}
}
