// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// runNeo4jAdoption runs bootstrap on Neo4j with schema adoption required and
// a catalog that already holds every expected object, including
// infra_search_key_ft, so adoption is complete and would write the marker.
func runNeo4jAdoption(t *testing.T, runner *fakeGraphRunner, events *[]string) (*fakeBootstrapDB, bool, error) {
	t.Helper()
	expected, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatal(err)
	}
	db := &fakeBootstrapDB{queryRows: []fakeBootstrapRows{{rows: nil}}}
	ddl := false
	var logs bytes.Buffer
	err = run(
		context.Background(),
		func(key string) string {
			switch key {
			case "ESHU_GRAPH_BACKEND":
				return "neo4j"
			case graphSchemaAdoptExistingEnv:
				return "true"
			}
			return ""
		},
		slog.New(slog.NewJSONHandler(&logs, nil)),
		func(context.Context, func(string) string) (bootstrapDB, error) { return db, nil },
		func(context.Context, bootstrapExecutor) error { return nil },
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor:  &fakeNeo4jExecutor{},
				inspector: &fakeGraphSchemaInspector{names: expected},
				runner:    runner,
				close:     func() error { return nil },
			}, nil
		},
		func(context.Context, graph.CypherExecutor, *slog.Logger, graph.SchemaBackend) error {
			ddl = true
			return nil
		},
	)
	return db, ddl, err
}

// TestAdoptionRunsTheInfraSearchKeyGateBeforeTheMarker proves adopting an
// existing Neo4j schema does not skip the #7245 gate: the oversized-key
// probe, the backfill, and the index-state check run before the adoption
// marker, exactly as on the DDL path.
func TestAdoptionRunsTheInfraSearchKeyGateBeforeTheMarker(t *testing.T) {
	t.Parallel()

	var events []string
	db, ddl, err := runNeo4jAdoption(t, &fakeGraphRunner{events: &events}, &events)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if ddl {
		t.Fatal("adoption applied DDL, want the existing schema adopted")
	}
	want := []string{"probe"}
	for range graph.InfraSearchKeyLabels() {
		want = append(want, "backfill")
	}
	want = append(want, "await_index", "index_state", "module_collapse", "module_repoint", "module_verify")
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v before the adoption marker", events, want)
	}
	if !markerWritten(db) {
		t.Fatal("adoption marker not written after a clean gate")
	}
}

// TestAdoptionRefusesWhileTheIndexIsNotOnline keeps the marker unset when an
// adopted infra_search_key_ft is FAILED (or still populating): adoption by
// name alone would otherwise trust a broken index.
func TestAdoptionRefusesWhileTheIndexIsNotOnline(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"FAILED", "POPULATING"} {
		var events []string
		db, _, err := runNeo4jAdoption(t, &fakeGraphRunner{events: &events, indexState: state}, &events)
		if err == nil || !strings.Contains(err.Error(), state) {
			t.Fatalf("run() with index %s error = %v, want a refusal naming the state", state, err)
		}
		if markerWritten(db) {
			t.Fatalf("adoption marker written over a %s index", state)
		}
	}
}

// TestAdoptionRefusesAnOversizedKey stops adoption before the marker when a
// key past the cap exists.
func TestAdoptionRefusesAnOversizedKey(t *testing.T) {
	t.Parallel()

	var events []string
	db, _, err := runNeo4jAdoption(t, &fakeGraphRunner{events: &events, oversized: 1}, &events)
	if err == nil || !strings.Contains(err.Error(), "1 infra search nodes") {
		t.Fatalf("run() error = %v, want the oversized-key refusal", err)
	}
	if markerWritten(db) {
		t.Fatal("adoption marker written with an oversized key")
	}
}
