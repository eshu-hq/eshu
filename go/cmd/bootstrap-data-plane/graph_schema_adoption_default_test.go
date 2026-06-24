// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

func TestRunAdoptsExistingNornicDBGraphSchemaByDefaultWhenMarkerMissing(t *testing.T) {
	t.Parallel()

	backend := graph.SchemaBackendNornicDB
	fingerprint, statementCount, err := graphSchemaFingerprint(backend)
	if err != nil {
		t.Fatalf("graphSchemaFingerprint() error = %v, want nil", err)
	}
	expectedNames, err := expectedGraphSchemaObjectNames(backend)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{{rows: nil}},
	}
	inspector := &fakeGraphSchemaInspector{names: expectedNames}
	logger := testLogger(t)
	graphApplied := false

	err = run(
		context.Background(),
		func(string) string { return "" },
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor:  &fakeNeo4jExecutor{},
				inspector: inspector,
				close:     func() error { return nil },
			}, nil
		},
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if graphApplied {
		t.Fatal("run() applied graph schema, want default existing-schema adoption")
	}
	if got, want := inspector.calls, 1; got != want {
		t.Fatalf("schema inspector calls = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("marker exec count = %d, want %d", got, want)
	}
	if got, want := db.execs[0].args[0], string(backend); got != want {
		t.Fatalf("mark backend arg = %q, want %q", got, want)
	}
	if got := db.execs[0].args[1]; got != fingerprint {
		t.Fatalf("mark fingerprint arg = %q, want %q", got, fingerprint)
	}
	if got := db.execs[0].args[2]; got != statementCount {
		t.Fatalf("mark statement count arg = %v, want %d", got, statementCount)
	}
}

func TestRunCanDisableDefaultNornicDBGraphSchemaAdoption(t *testing.T) {
	t.Parallel()

	expectedNames, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{{rows: nil}},
	}
	inspector := &fakeGraphSchemaInspector{names: expectedNames}
	logger := testLogger(t)
	graphApplied := false

	err = run(
		context.Background(),
		func(key string) string {
			if key == graphSchemaAdoptExistingEnv {
				return "false"
			}
			return ""
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor:  &fakeNeo4jExecutor{},
				inspector: inspector,
				close:     func() error { return nil },
			}, nil
		},
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !graphApplied {
		t.Fatal("run() did not apply graph schema after adoption was disabled")
	}
	if got, want := inspector.calls, 0; got != want {
		t.Fatalf("schema inspector calls = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("marker exec count = %d, want %d", got, want)
	}
}

func TestRunDoesNotDefaultAdoptNeo4jGraphSchema(t *testing.T) {
	t.Parallel()

	expectedNames, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v, want nil", err)
	}
	db := &fakeBootstrapDB{
		queryRows: []fakeBootstrapRows{{rows: nil}},
	}
	inspector := &fakeGraphSchemaInspector{names: expectedNames}
	logger := testLogger(t)
	graphApplied := false

	err = run(
		context.Background(),
		func(key string) string {
			if key == "ESHU_GRAPH_BACKEND" {
				return "neo4j"
			}
			return ""
		},
		logger,
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapExecutor) error {
			return nil
		},
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor:  &fakeNeo4jExecutor{},
				inspector: inspector,
				close:     func() error { return nil },
			}, nil
		},
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !graphApplied {
		t.Fatal("run() did not apply graph schema for default Neo4j bootstrap")
	}
	if got, want := inspector.calls, 0; got != want {
		t.Fatalf("schema inspector calls = %d, want %d", got, want)
	}
}
