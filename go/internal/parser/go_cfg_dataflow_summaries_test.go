package parser

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestGoDataflowSummariesEmitEffects proves the parser emits a per-function
// dataflow_summaries bucket carrying each function's structural value-flow
// effects: query's request parameter reaching a SQL sink, and handle passing its
// request parameter into query's argument (the TITO callee-arg flow that
// cross-repo composition consumes).
func TestGoDataflowSummariesEmitEffects(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

import (
	"database/sql"
	"net/http"
)

func handle(r *http.Request, db *sql.DB) {
	query(db, r)
}

func query(db *sql.DB, r *http.Request) {
	db.Query(r.FormValue("q"))
}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{
		EmitDataflow:        true,
		RepositoryID:        "repo-alpha",
		GoPackageImportPath: "example.com/repo/handlers",
	})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}

	rows, ok := got["dataflow_summaries"].([]map[string]any)
	if !ok {
		t.Fatalf("dataflow_summaries bucket missing or wrong type: %T", got["dataflow_summaries"])
	}

	byName := func(fragment string) map[string]any {
		for _, row := range rows {
			if id, _ := row["function_id"].(string); strings.Contains(id, fragment) {
				return row
			}
		}
		return nil
	}

	query := byName("query")
	if query == nil {
		t.Fatalf("no summary row for query: %+v", rows)
	}
	queryID, _ := query["function_id"].(string)
	if !strings.HasPrefix(queryID, "repo-alpha\x1fexample.com/repo/handlers\x1f") {
		t.Fatalf("query summary FunctionID missing durable repo/package prefix: %q", queryID)
	}
	sinks, _ := query["param_to_sink"].([]map[string]any)
	hasSQLSink := false
	for _, s := range sinks {
		if kind, _ := s["sink_kind"].(string); kind == "sql" {
			hasSQLSink = true
		}
	}
	if !hasSQLSink {
		t.Fatalf("query summary missing a sql param_to_sink: %+v", query)
	}

	// Pick the handle row that carries a callee-arg flow into query.
	var handle map[string]any
	for _, row := range rows {
		id, _ := row["function_id"].(string)
		calls, _ := row["param_to_call_arg"].([]map[string]any)
		if strings.Contains(id, "handle") && len(calls) > 0 {
			handle = row
		}
	}
	if handle == nil {
		t.Fatalf("no handle summary with a param_to_call_arg flow into query: %+v", rows)
	}
	calls, _ := handle["param_to_call_arg"].([]map[string]any)
	calleeIntoQuery := false
	for _, c := range calls {
		if callee, _ := c["callee"].(string); strings.Contains(callee, "query") {
			calleeIntoQuery = true
		}
	}
	if !calleeIntoQuery {
		t.Fatalf("handle summary param_to_call_arg does not reference query: %+v", handle)
	}
}

// TestGoDataflowSourcesEmitParamEntryPoints proves the parser emits durable
// param-level source entry points for the reducer fixpoint alongside summaries.
func TestGoDataflowSourcesEmitParamEntryPoints(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

import "net/http"

func handle(r *http.Request) string {
	return r.FormValue("q")
}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{
		EmitDataflow:        true,
		RepositoryID:        "repo-alpha",
		GoPackageImportPath: "example.com/repo/handlers",
	})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}

	rows, ok := got["dataflow_sources"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("dataflow_sources = %T %#v, want one row", got["dataflow_sources"], got["dataflow_sources"])
	}
	row := rows[0]
	id, _ := row["function_id"].(string)
	if !strings.HasPrefix(id, "repo-alpha\x1fexample.com/repo/handlers\x1f") || !strings.Contains(id, "handle") {
		t.Fatalf("source FunctionID = %q, want durable handle id", id)
	}
	if got, want := row["param_index"], 0; got != want {
		t.Fatalf("param_index = %#v, want %d", got, want)
	}
	if got, want := row["source_kind"], "http_request"; got != want {
		t.Fatalf("source_kind = %#v, want %q", got, want)
	}
}

func TestGoDataflowSourcesRequireDurableIdentity(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

import "net/http"

func handle(r *http.Request) string {
	return r.FormValue("q")
}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{
		EmitDataflow: true,
		RepositoryID: "repo-alpha",
	})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	if _, present := got["dataflow_sources"]; present {
		t.Fatalf("dataflow_sources emitted without Go package import path: %+v", got["dataflow_sources"])
	}
}

// TestGoDataflowSummariesSortedByID proves the summaries bucket is byte-stable:
// rows are ordered by function_id.
func TestGoDataflowSummariesSortedByID(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "many.go")
	writeTestFile(t, filePath, `package many

func zulu(x string) string { return x }
func alpha(x string) string { return x }
func mike(x string) string { return x }
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{
		EmitDataflow:        true,
		RepositoryID:        "repo-alpha",
		GoPackageImportPath: "example.com/repo/many",
	})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	rows, ok := got["dataflow_summaries"].([]map[string]any)
	if !ok || len(rows) < 3 {
		t.Fatalf("expected >=3 summary rows, got %T %d", got["dataflow_summaries"], len(rows))
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id, _ := row["function_id"].(string)
		ids = append(ids, id)
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("dataflow_summaries not sorted by function_id: %v", ids)
	}
}

// TestGoDataflowSummariesRequireRepositoryID proves the parser does not emit
// durable summary rows without the repository component that the persistence
// layer requires for FunctionID stability. Other opt-in dataflow buckets can
// still be emitted for direct parser callers.
func TestGoDataflowSummariesRequireRepositoryID(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

func handle(x string) string { return x }
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{
		EmitDataflow:        true,
		GoPackageImportPath: "example.com/repo/handlers",
	})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	if _, present := got["dataflow_summaries"]; present {
		t.Fatalf("dataflow_summaries emitted without repository id: %+v", got["dataflow_summaries"])
	}
}

// TestGoDataflowSummariesRequireGoPackageImportPath proves durable Go
// FunctionIDs keep their package component; repository identity alone is not
// enough to disambiguate functions with the same name across packages.
func TestGoDataflowSummariesRequireGoPackageImportPath(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

func handle(x string) string { return x }
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true, RepositoryID: "repo-alpha"})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	if _, present := got["dataflow_summaries"]; present {
		t.Fatalf("dataflow_summaries emitted without Go package import path: %+v", got["dataflow_summaries"])
	}
}
