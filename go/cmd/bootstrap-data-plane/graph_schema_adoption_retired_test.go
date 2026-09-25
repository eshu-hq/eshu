// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// TestRunDoesNotAdoptNeo4jSchemaThatStillHasRetiredConstraints guards #7095:
// the Neo4j schema drops uniqueness constraints narrower than the canonical
// uid. A store that has every current object but still carries a retired
// constraint is not the current schema; adopting it would record the marker
// without running the DROP, and the moved-block dead letter would persist.
func TestRunDoesNotAdoptNeo4jSchemaThatStillHasRetiredConstraints(t *testing.T) {
	t.Parallel()

	expectedNames, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v, want nil", err)
	}
	if _, ok := expectedNames["tf_module_unique"]; ok {
		t.Fatal("expected Neo4j schema objects include retired tf_module_unique")
	}
	names := make(map[string]struct{}, len(expectedNames)+1)
	for name := range expectedNames {
		names[name] = struct{}{}
	}
	names["tf_module_unique"] = struct{}{}

	db := &fakeBootstrapDB{queryRows: []fakeBootstrapRows{{rows: nil}}}
	inspector := &fakeGraphSchemaInspector{names: names}
	graphApplied := false
	err = run(
		context.Background(),
		func(key string) string {
			switch key {
			case graphSchemaAdoptExistingEnv:
				return "true"
			case "ESHU_GRAPH_BACKEND":
				return "neo4j"
			}
			return ""
		},
		testLogger(t),
		func(context.Context, func(string) string) (bootstrapDB, error) { return db, nil },
		func(context.Context, bootstrapExecutor) error { return nil },
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{executor: &fakeNeo4jExecutor{}, inspector: inspector, close: func() error { return nil }}, nil
		},
		func(_ context.Context, _ graph.CypherExecutor, _ *slog.Logger, _ graph.SchemaBackend) error {
			graphApplied = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if got, want := inspector.calls, 1; got != want {
		t.Fatalf("schema inspector calls = %d, want %d", got, want)
	}
	if !graphApplied {
		t.Fatal("run() adopted a Neo4j schema that still has retired constraint tf_module_unique; want the schema applied so the DROP runs")
	}
}
