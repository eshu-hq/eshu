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
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// fakeGraphRunner answers the bootstrap infra search key statements and
// records their order in a shared event log.
type fakeGraphRunner struct {
	events     *[]string
	oversized  int64
	indexState string
	// awaitErr and stateErr make db.awaitIndex or the state read fail.
	awaitErr error
	stateErr error
	// moduleUnkeyed is what the Module import_key verify step reports, and
	// moduleErr makes the collapse step fail.
	moduleUnkeyed int64
	moduleAdopted int64
	moduleErr     error
	// moduleVerifyEmpty makes the verify step return no row.
	moduleVerifyEmpty bool
	// moduleNonImports is what the Module import_key collapse step reports
	// for non-IMPORTS relationships on deleted duplicates.
	moduleNonImports int64
}

func (r *fakeGraphRunner) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	switch {
	case cypher == moduleImportKeyCollapseCypher:
		*r.events = append(*r.events, "module_collapse")
		return []map[string]any{{"keyed": int64(0), "on_duplicates": int64(0), "deleted": int64(0), "non_imports": r.moduleNonImports}}, r.moduleErr
	case cypher == moduleImportKeyRepointCypher:
		*r.events = append(*r.events, "module_repoint")
		return []map[string]any{{"repointed": int64(0)}}, nil
	case cypher == moduleImportKeyVerifyCypher:
		*r.events = append(*r.events, "module_verify")
		if r.moduleVerifyEmpty {
			return nil, nil
		}
		return []map[string]any{{"unkeyed": r.moduleUnkeyed, "adopted_imports": r.moduleAdopted}}, nil
	case cypher == search.OversizedKeyCountCypher():
		*r.events = append(*r.events, "probe")
		return []map[string]any{{"oversized": r.oversized}}, nil
	case strings.Contains(cypher, "IN TRANSACTIONS"):
		*r.events = append(*r.events, "backfill")
		return []map[string]any{{"keyed": int64(1), "overflowed": int64(0)}}, nil
	case strings.HasPrefix(cypher, "CALL db.awaitIndex("):
		*r.events = append(*r.events, "await_index")
		return nil, r.awaitErr
	case strings.HasPrefix(cypher, "SHOW FULLTEXT INDEXES"):
		*r.events = append(*r.events, "index_state")
		if r.stateErr != nil {
			return nil, r.stateErr
		}
		state := r.indexState
		if state == "" {
			state = "ONLINE"
		}
		return []map[string]any{{"name": graph.InfraSearchKeyIndexName, "state": state, "populationPercent": 100.0}}, nil
	}
	*r.events = append(*r.events, "other")
	return nil, nil
}

func runWithGraphRunner(t *testing.T, backend string, runner *fakeGraphRunner, events *[]string, logs *bytes.Buffer) (*fakeBootstrapDB, error) {
	t.Helper()
	db := &fakeBootstrapDB{}
	err := run(
		context.Background(),
		func(key string) string {
			if key == "ESHU_GRAPH_BACKEND" {
				return backend
			}
			return ""
		},
		slog.New(slog.NewJSONHandler(logs, nil)),
		func(context.Context, func(string) string) (bootstrapDB, error) { return db, nil },
		func(context.Context, bootstrapExecutor) error { return nil },
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{executor: &fakeNeo4jExecutor{}, runner: runner, close: func() error { return nil }}, nil
		},
		func(context.Context, graph.CypherExecutor, *slog.Logger, graph.SchemaBackend) error {
			*events = append(*events, "ddl")
			return nil
		},
	)
	return db, err
}

// TestRunProbesBeforeDDLAndBackfillsBeforeTheMarker pins the Neo4j order:
// the oversized-key probe runs before any DDL (so the FULLTEXT index is never
// created over a key past the cap), the backfill and the index-state check run
// after DDL, and the schema marker is written last.
func TestRunProbesBeforeDDLAndBackfillsBeforeTheMarker(t *testing.T) {
	t.Parallel()

	var events []string
	var logs bytes.Buffer
	db, err := runWithGraphRunner(t, "neo4j", &fakeGraphRunner{events: &events}, &events, &logs)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	want := []string{"probe", "ddl"}
	for range graph.InfraSearchKeyLabels() {
		want = append(want, "backfill")
	}
	want = append(want, "await_index", "index_state", "module_collapse", "module_repoint", "module_verify")
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if !markerWritten(db) {
		t.Fatal("schema marker not written after a clean backfill")
	}
	for _, event := range []string{"bootstrap.graph.infra_search_key.readiness", "bootstrap.graph.infra_search_key.backfilled", "bootstrap.graph.infra_search_key.index_state"} {
		if !strings.Contains(logs.String(), event) {
			t.Errorf("logs missing %s", event)
		}
	}
	if !strings.Contains(logs.String(), `"population_percent":100`) || !strings.Contains(logs.String(), `"oversized_keys":0`) {
		t.Errorf("logs missing index population or probe count: %s", logs.String())
	}
}

// TestRunRefusesTheIndexWhenAKeyIsOverTheCap proves a non-zero probe fails
// the run loudly before any DDL and before the marker.
func TestRunRefusesTheIndexWhenAKeyIsOverTheCap(t *testing.T) {
	t.Parallel()

	var events []string
	var logs bytes.Buffer
	db, err := runWithGraphRunner(t, "neo4j", &fakeGraphRunner{events: &events, oversized: 3}, &events, &logs)
	if err == nil || !strings.Contains(err.Error(), "3 infra search nodes") {
		t.Fatalf("run() error = %v, want the oversized-key refusal", err)
	}
	if strings.Join(events, ",") != "probe" {
		t.Fatalf("events = %v, want only the probe", events)
	}
	if markerWritten(db) {
		t.Fatal("schema marker written after a refused probe")
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("refusal not logged at ERROR: %s", logs.String())
	}
}

// TestRunFailsWhenTheIndexIsFailed keeps the marker unset when the index
// reports FAILED after the backfill.
func TestRunFailsWhenTheIndexIsFailed(t *testing.T) {
	t.Parallel()

	var events []string
	var logs bytes.Buffer
	db, err := runWithGraphRunner(t, "neo4j", &fakeGraphRunner{events: &events, indexState: "FAILED"}, &events, &logs)
	if err == nil || !strings.Contains(err.Error(), "FAILED") {
		t.Fatalf("run() error = %v, want the FAILED index error", err)
	}
	if markerWritten(db) {
		t.Fatal("schema marker written over a FAILED index")
	}
}

// TestRunSkipsTheInfraSearchKeyOnNornicDB keeps NornicDB unchanged: no probe,
// no backfill, no index-state read.
func TestRunSkipsTheInfraSearchKeyOnNornicDB(t *testing.T) {
	t.Parallel()

	var events []string
	var logs bytes.Buffer
	if _, err := runWithGraphRunner(t, "nornicdb", &fakeGraphRunner{events: &events}, &events, &logs); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if strings.Join(events, ",") != "ddl" {
		t.Fatalf("events = %v, want only the DDL", events)
	}
}

func markerWritten(db *fakeBootstrapDB) bool {
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "graph_schema_applications") {
			return true
		}
	}
	return false
}
