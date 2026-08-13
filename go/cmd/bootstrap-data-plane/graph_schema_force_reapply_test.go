// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// TestRunReappliesGraphSchemaWhenForcedAfterGraphWipe covers the disaster-recovery
// case the marker cannot see. The "has the graph schema been applied?" answer
// lives in Postgres (graph_schema_applications), and the DR sequence preserves
// Postgres while wiping the graph. So after a wipe the marker still matches, run
// returns before it ever opens a graph connection, and the rebuild projects
// millions of nodes into a graph with no indexes or constraints.
//
// ESHU_GRAPH_SCHEMA_FORCE_REAPPLY is the operator's way to say "the marker is
// lying, the graph is empty". With it set, schema must be applied even on a
// fingerprint match.
func TestRunReappliesGraphSchemaWhenForcedAfterGraphWipe(t *testing.T) {
	t.Parallel()

	backend := graph.SchemaBackendNornicDB
	fingerprint, _, err := graphSchemaFingerprint(backend)
	if err != nil {
		t.Fatalf("graphSchemaFingerprint() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{
			{rows: [][]any{{fingerprint, []byte(`[]`)}}},
		},
	}
	graphApplied := false

	err = run(
		context.Background(),
		func(key string) string {
			switch key {
			case graphSchemaForceReapplyEnv:
				return "true"
			case graphSchemaAdoptExistingEnv:
				// Adoption would short-circuit on a graph that still reported the
				// objects; disable it so this test pins the marker bypass alone.
				return "false"
			default:
				return ""
			}
		},
		testLogger(t),
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		noopNeo4j,
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !graphApplied {
		t.Fatal("run() skipped graph schema despite the force flag: a rebuild after a graph wipe would run with no indexes or constraints")
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("marker exec count = %d, want %d (the marker must be refreshed after a forced reapply)", got, want)
	}
}

// TestRunKeepsMarkerSkipWhenForceReapplyUnset is the other half of the pair. The
// marker skip exists because re-running CREATE CONSTRAINT against a large
// retained graph costs minutes per constraint, so an unset flag must keep the
// fast path.
func TestRunKeepsMarkerSkipWhenForceReapplyUnset(t *testing.T) {
	t.Parallel()

	fingerprint, _, err := graphSchemaFingerprint(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("graphSchemaFingerprint() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{
			{rows: [][]any{{fingerprint, []byte(`[]`)}}},
		},
	}
	graphApplied := false

	err = run(
		context.Background(),
		func(string) string { return "" },
		testLogger(t),
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		noopNeo4j,
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if graphApplied {
		t.Fatal("run() reapplied graph schema without the force flag, losing the marker fast path")
	}
}
