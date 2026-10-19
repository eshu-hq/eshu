// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// orderedSchemaDB is a graphSchemaBootstrapDB that records the marker write in
// the shared event order.
type orderedSchemaDB struct {
	graphSchemaBootstrapDB
	events *[]string
}

func (f *orderedSchemaDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	*f.events = append(*f.events, "mark")
	return f.graphSchemaBootstrapDB.ExecContext(ctx, query, args...)
}

// orderedSchemaExecutor records the DDL statements as one "ddl" event.
type orderedSchemaExecutor struct{ events *[]string }

func (e orderedSchemaExecutor) ExecuteCypher(context.Context, graph.CypherStatement) error {
	if n := len(*e.events); n == 0 || (*e.events)[n-1] != "ddl" {
		*e.events = append(*e.events, "ddl")
	}
	return nil
}

// fakeModuleCheckReader answers the Module import_key pending check.
type fakeModuleCheckReader struct {
	events           *[]string
	unkeyed, adopted int64
	err              error
}

func (r fakeModuleCheckReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	if cypher != sourcecypher.CanonicalModuleImportKeyPendingCypher {
		return nil, errors.New("unexpected statement: " + cypher)
	}
	*r.events = append(*r.events, "module_check")
	if r.err != nil {
		return nil, r.err
	}
	return []map[string]any{{"unkeyed": r.unkeyed, "adopted_imports": r.adopted}}, nil
}

// staticRowsReader returns fixed rows, to feed the check a malformed answer.
type staticRowsReader []map[string]any

func (r staticRowsReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return r, nil
}

func ensureSchemaWithModuleCheck(t *testing.T, backend graph.SchemaBackend, reader graphRowReader, events *[]string) (*orderedSchemaDB, error) {
	t.Helper()
	database := &orderedSchemaDB{
		graphSchemaBootstrapDB: graphSchemaBootstrapDB{app: graph.MustSchemaApplicationForBackend(backend), missingUntilMarked: true},
		events:                 events,
	}
	err := ensureBootstrapGraphSchemaWithOpener(
		context.Background(),
		database,
		func(string) string { return "" },
		nil,
		func(context.Context, func(string) string) (bootstrapGraphSchema, error) {
			return bootstrapGraphSchema{
				backend:  backend,
				executor: orderedSchemaExecutor{events: events},
				reader:   reader,
				close:    func() error { return nil },
			}, nil
		},
	)
	return database, err
}

// TestEnsureBootstrapGraphSchemaRefusesTheMarkerOverUnmigratedModules proves
// bootstrap-index cannot readmit import_key writers into a populated graph
// that lost its marker: on Neo4j it runs the Module import_key check after the
// DDL and before the marker, and unmigrated canonical Modules leave the marker
// unset with an error that names the binary that migrates them.
func TestEnsureBootstrapGraphSchemaRefusesTheMarkerOverUnmigratedModules(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name             string
		unkeyed, adopted int64
		want             []string
	}{
		{name: "unkeyed canonical nodes", unkeyed: 197, want: []string{"197 unkeyed canonical Module nodes", "0 IMPORTS edges"}},
		{name: "adopted imports only", adopted: 13, want: []string{"0 unkeyed canonical Module nodes", "13 IMPORTS edges"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var events []string
			reader := fakeModuleCheckReader{events: &events, unkeyed: tc.unkeyed, adopted: tc.adopted}
			database, err := ensureSchemaWithModuleCheck(t, graph.SchemaBackendNeo4j, reader, &events)
			if err == nil {
				t.Fatal("ensureBootstrapGraphSchemaWithOpener() error = nil, want the unmigrated-Module refusal")
			}
			for _, want := range append(tc.want, "eshu-bootstrap-data-plane", "deployment-contract") {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, missing %q", err.Error(), want)
				}
			}
			if database.marked {
				t.Fatalf("marker written over unmigrated Modules; events = %v", events)
			}
			if got := strings.Join(events, ","); got != "ddl,module_check" {
				t.Fatalf("events = %s, want ddl,module_check", got)
			}
		})
	}
}

// TestEnsureBootstrapGraphSchemaChecksModulesBeforeTheMarker proves the clean
// Neo4j path still marks, and only after the check ran.
func TestEnsureBootstrapGraphSchemaChecksModulesBeforeTheMarker(t *testing.T) {
	t.Parallel()

	var events []string
	database, err := ensureSchemaWithModuleCheck(t, graph.SchemaBackendNeo4j, fakeModuleCheckReader{events: &events}, &events)
	if err != nil {
		t.Fatalf("ensureBootstrapGraphSchemaWithOpener() error = %v", err)
	}
	if !database.marked {
		t.Fatal("marker not written on a graph with no unmigrated Modules")
	}
	if got := strings.Join(events, ","); got != "ddl,module_check,mark" {
		t.Fatalf("events = %s, want ddl,module_check,mark", got)
	}
}

// TestEnsureBootstrapGraphSchemaFailsClosedWhenTheModuleCheckCannotRun proves
// a failed check, a malformed answer, or a Neo4j handle with no reader never
// marks.
func TestEnsureBootstrapGraphSchemaFailsClosedWhenTheModuleCheckCannotRun(t *testing.T) {
	t.Parallel()

	var events []string
	database, err := ensureSchemaWithModuleCheck(t, graph.SchemaBackendNeo4j,
		fakeModuleCheckReader{events: &events, err: errors.New("leader switch")}, &events)
	if err == nil || !strings.Contains(err.Error(), "leader switch") {
		t.Fatalf("error = %v, want the check failure", err)
	}
	if database.marked {
		t.Fatal("marker written after a failed Module check")
	}

	for _, rows := range [][]map[string]any{nil, {{"unkeyed": "0", "adopted_imports": int64(0)}}} {
		var malformedEvents []string
		database, err = ensureSchemaWithModuleCheck(t, graph.SchemaBackendNeo4j, staticRowsReader(rows), &malformedEvents)
		if err == nil || database.marked {
			t.Fatalf("rows %v: error = %v, marked = %v; want a refusal and no marker", rows, err, database.marked)
		}
	}

	var noReaderEvents []string
	database, err = ensureSchemaWithModuleCheck(t, graph.SchemaBackendNeo4j, nil, &noReaderEvents)
	if err == nil {
		t.Fatal("error = nil, want a refusal when Neo4j gives no reader for the Module check")
	}
	if database.marked {
		t.Fatal("marker written with no Module check")
	}
}

// TestEnsureBootstrapGraphSchemaSkipsTheModuleCheckOnNornicDB proves the check
// follows the migration's Neo4j-only scope: NornicDB is not supported for
// deployment and has no populated-store migration to point at.
func TestEnsureBootstrapGraphSchemaSkipsTheModuleCheckOnNornicDB(t *testing.T) {
	t.Parallel()

	var events []string
	database, err := ensureSchemaWithModuleCheck(t, graph.SchemaBackendNornicDB, nil, &events)
	if err != nil {
		t.Fatalf("ensureBootstrapGraphSchemaWithOpener() error = %v", err)
	}
	if !database.marked {
		t.Fatal("NornicDB marker not written")
	}
	if got := strings.Join(events, ","); got != "ddl,mark" {
		t.Fatalf("events = %s, want ddl,mark", got)
	}
}
