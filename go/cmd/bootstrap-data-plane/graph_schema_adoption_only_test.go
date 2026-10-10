// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/envregistry"
	"github.com/eshu-hq/eshu/go/internal/graph"
)

func TestGraphSchemaAdoptOnlyBooleanMatchesRegistry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"1", true},
		{"t", true},
		{"T", true},
		{"TRUE", true},
		{"true", true},
		{"True", true},
		{"0", false},
		{"f", false},
		{"F", false},
		{"FALSE", false},
		{"false", false},
		{"False", false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			if findings := envregistry.Default().Validate(map[string]string{graphSchemaAdoptOnlyEnv: tc.raw}, true); len(findings) != 0 {
				t.Fatalf("registry rejects %q: %v", tc.raw, findings)
			}
			got, err := graphSchemaAdoptOnly(func(key string) string {
				if key == graphSchemaAdoptOnlyEnv {
					return tc.raw
				}
				return ""
			})
			if err != nil || got != tc.want {
				t.Fatalf("graphSchemaAdoptOnly(%q) = %v, %v; want %v, nil", tc.raw, got, err, tc.want)
			}
		})
	}
	for _, raw := range []string{" true ", "\tFALSE\n"} {
		t.Run("padded_"+strings.TrimSpace(raw), func(t *testing.T) {
			t.Parallel()
			_, err := graphSchemaAdoptOnly(func(key string) string {
				if key == graphSchemaAdoptOnlyEnv {
					return raw
				}
				return ""
			})
			if err != nil {
				t.Fatalf("graphSchemaAdoptOnly(%q): %v", raw, err)
			}
		})
	}
	for _, raw := range []string{"", " ", "yes", "on", "TrUe"} {
		t.Run("invalid_or_unset_"+raw, func(t *testing.T) {
			t.Parallel()
			got, err := graphSchemaAdoptOnly(func(key string) string {
				if key == graphSchemaAdoptOnlyEnv {
					return raw
				}
				return ""
			})
			if raw == "" {
				if got || err != nil {
					t.Fatalf("unset graphSchemaAdoptOnly() = %v, %v; want false, nil", got, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("graphSchemaAdoptOnly(%q) = %v, nil; want error", raw, got)
			}
			if findings := envregistry.Default().Validate(map[string]string{graphSchemaAdoptOnlyEnv: raw}, true); len(findings) == 0 {
				t.Fatalf("registry accepts invalid value %q", raw)
			}
		})
	}
}

// TestRunAdoptionOnlyRejectsIncompleteNeo4jSchemaWithoutDDL proves the
// migration-only Job cannot silently turn a catalog mismatch into graph DDL.
func TestRunAdoptionOnlyRejectsIncompleteNeo4jSchemaWithoutDDL(t *testing.T) {
	t.Parallel()

	expected, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v", err)
	}
	if _, ok := expected["repository_id"]; !ok {
		t.Fatal("Neo4j schema lacks repository_id; update the missing-object fixture")
	}
	for _, tc := range []struct {
		name       string
		alter      func(map[string]struct{})
		markerRows [][]any
	}{
		{name: "missing", alter: func(names map[string]struct{}) { delete(names, "repository_id") }},
		{name: "retired", alter: func(names map[string]struct{}) { names["tf_module_unique"] = struct{}{} }},
		{name: "incompatible_marker_and_missing", alter: func(names map[string]struct{}) { delete(names, "repository_id") }, markerRows: [][]any{{"older-incompatible-fingerprint", []byte(`[]`)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names := maps.Clone(expected)
			tc.alter(names)
			database := &fakeBootstrapDB{queryRows: []fakeBootstrapRows{{rows: tc.markerRows}}}
			inspector := &fakeGraphSchemaInspector{names: names}
			graphDDLCalls := 0
			err := run(
				context.Background(),
				func(key string) string {
					switch key {
					case "ESHU_GRAPH_SCHEMA_ADOPT_ONLY":
						return "true"
					case "ESHU_GRAPH_BACKEND":
						return "neo4j"
					}
					return ""
				},
				testLogger(t),
				func(context.Context, func(string) string) (bootstrapDB, error) { return database, nil },
				func(context.Context, bootstrapExecutor) error { return nil },
				func(context.Context, func(string) string) (neo4jDeps, error) {
					return neo4jDeps{
						executor:  &fakeNeo4jExecutor{},
						inspector: inspector,
						close:     func() error { return nil },
					}, nil
				},
				func(context.Context, graph.CypherExecutor, *slog.Logger, graph.SchemaBackend) error {
					graphDDLCalls++
					return nil
				},
			)
			if err == nil {
				t.Fatal("run() succeeded with incomplete schema in adoption-only mode")
			}
			if graphDDLCalls != 0 {
				t.Fatalf("graph DDL calls = %d, want zero", graphDDLCalls)
			}
			if len(database.execs) != 0 {
				t.Fatalf("marker writes = %d, want zero", len(database.execs))
			}
		})
	}
}

func TestRunAdoptionOnlyRejectsInvalidConfigurationBeforeOpeningStores(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		env  map[string]string
	}{
		{"invalid_only", map[string]string{"ESHU_GRAPH_SCHEMA_ADOPT_ONLY": "perhaps"}},
		{"whitespace_only", map[string]string{"ESHU_GRAPH_SCHEMA_ADOPT_ONLY": " \t "}},
		{"legacy_yes", map[string]string{"ESHU_GRAPH_SCHEMA_ADOPT_ONLY": "yes"}},
		{"legacy_on", map[string]string{"ESHU_GRAPH_SCHEMA_ADOPT_ONLY": "on"}},
		{"mixed_case", map[string]string{"ESHU_GRAPH_SCHEMA_ADOPT_ONLY": "TrUe"}},
		{"forced_reapply", map[string]string{"ESHU_GRAPH_SCHEMA_ADOPT_ONLY": "true", graphSchemaForceReapplyEnv: "yes"}},
		{"disabled_adoption", map[string]string{"ESHU_GRAPH_SCHEMA_ADOPT_ONLY": "true", graphSchemaAdoptExistingEnv: "false"}},
		{"invalid_adoption", map[string]string{"ESHU_GRAPH_SCHEMA_ADOPT_ONLY": "true", graphSchemaAdoptExistingEnv: "perhaps"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opened := false
			err := run(context.Background(), func(key string) string { return tc.env[key] }, testLogger(t),
				func(context.Context, func(string) string) (bootstrapDB, error) {
					opened = true
					return nil, errors.New("unexpected Postgres open")
				},
				func(context.Context, bootstrapExecutor) error { return nil },
				func(context.Context, func(string) string) (neo4jDeps, error) {
					opened = true
					return neo4jDeps{}, errors.New("unexpected graph open")
				},
				func(context.Context, graph.CypherExecutor, *slog.Logger, graph.SchemaBackend) error {
					t.Fatal("unexpected graph DDL")
					return nil
				})
			if err == nil || opened {
				t.Fatalf("run() error = %v, opened stores = %v; want config error before open", err, opened)
			}
		})
	}
}

func TestRunAdoptionOnlyHandlesCompleteAndUnavailableCatalog(t *testing.T) {
	t.Parallel()
	expected, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		adoptExisting string
		inspector     *fakeGraphSchemaInspector
		wantMarker    bool
	}{
		{"complete_unset", "", &fakeGraphSchemaInspector{names: maps.Clone(expected)}, true},
		{"complete_explicit", "true", &fakeGraphSchemaInspector{names: maps.Clone(expected)}, true},
		{"missing_inspector", "", nil, false},
		{"inspector_error", "", &fakeGraphSchemaInspector{err: errors.New("catalog unavailable")}, false},
		{"inspector_timeout", "", &fakeGraphSchemaInspector{err: context.DeadlineExceeded}, false},
		{"inspector_cancel", "", &fakeGraphSchemaInspector{err: context.Canceled}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			database := &fakeBootstrapDB{queryRows: []fakeBootstrapRows{{rows: nil}}}
			graphDDLCalls := 0
			err := run(context.Background(), func(key string) string {
				switch key {
				case "ESHU_GRAPH_SCHEMA_ADOPT_ONLY":
					return "true"
				case graphSchemaAdoptExistingEnv:
					return tc.adoptExisting
				case "ESHU_GRAPH_BACKEND":
					return "neo4j"
				}
				return ""
			}, testLogger(t),
				func(context.Context, func(string) string) (bootstrapDB, error) { return database, nil },
				func(context.Context, bootstrapExecutor) error { return nil },
				func(context.Context, func(string) string) (neo4jDeps, error) {
					deps := neo4jDeps{executor: &fakeNeo4jExecutor{}, close: func() error { return nil }}
					if tc.inspector != nil {
						deps.inspector = tc.inspector
					}
					return deps, nil
				},
				func(context.Context, graph.CypherExecutor, *slog.Logger, graph.SchemaBackend) error {
					graphDDLCalls++
					return nil
				})
			if (err == nil) != tc.wantMarker {
				t.Fatalf("run() error = %v, want success = %v", err, tc.wantMarker)
			}
			if graphDDLCalls != 0 || (len(database.execs) == 1) != tc.wantMarker {
				t.Fatalf("graph DDL = %d, marker writes = %d; want marker = %v", graphDDLCalls, len(database.execs), tc.wantMarker)
			}
			if !tc.wantMarker && tc.inspector == nil && !strings.Contains(err.Error(), "inspection support") {
				t.Fatalf("run() error = %v, want inspection support refusal", err)
			}
		})
	}
}

func TestRunAdoptionOnlyKeepsCompatibleMarkerSkip(t *testing.T) {
	t.Parallel()
	app, err := graph.SchemaApplicationForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		fingerprint string
		compat      []byte
		wantRefresh int
	}{
		{"exact", app.Fingerprint, []byte(`[]`), 1},
		{"compatible", "future-additive-fingerprint", []byte(`["` + app.Fingerprint + `"]`), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			database := &fakeBootstrapDB{queryRows: []fakeBootstrapRows{{rows: [][]any{{tc.fingerprint, tc.compat}}}}}
			graphOpened := false
			err := run(context.Background(), func(key string) string {
				if key == "ESHU_GRAPH_SCHEMA_ADOPT_ONLY" {
					return "true"
				}
				if key == "ESHU_GRAPH_BACKEND" {
					return "neo4j"
				}
				return ""
			}, testLogger(t),
				func(context.Context, func(string) string) (bootstrapDB, error) { return database, nil },
				func(context.Context, bootstrapExecutor) error { return nil },
				func(context.Context, func(string) string) (neo4jDeps, error) {
					graphOpened = true
					return neo4jDeps{}, errors.New("unexpected graph open")
				},
				func(context.Context, graph.CypherExecutor, *slog.Logger, graph.SchemaBackend) error {
					t.Fatal("unexpected graph DDL")
					return nil
				})
			if err != nil || graphOpened || len(database.execs) != tc.wantRefresh {
				t.Fatalf("run() error = %v, graph opened = %v, marker refreshes = %d; want %d", err, graphOpened, len(database.execs), tc.wantRefresh)
			}
		})
	}
}
