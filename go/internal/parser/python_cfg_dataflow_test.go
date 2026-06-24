// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// pythonDataflowFixture exercises both an intraprocedural flow (request.GET into
// cursor.execute within view) and an interprocedural flow (request passed into
// run_query, whose parameter reaches a cursor.execute sink).
const pythonDataflowFixture = `from fastapi import Request

def view(request: Request, db):
    q = request.GET
    cursor.execute(q)
    run_query(db, request)

def run_query(db, q):
    cursor.execute(q)
`

// TestPythonDataflowOffIsByteIdentical proves the value-flow gate is byte-
// identical when off: enabling it adds exactly the opt-in buckets and changes
// nothing else, so the existing Python fact contract is untouched by default.
func TestPythonDataflowOffIsByteIdentical(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "views.py")
	writeTestFile(t, filePath, pythonDataflowFixture)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	off, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath (off) error = %v", err)
	}
	for _, bucket := range []string{"dataflow_catalog_versions", "dataflow_functions", "taint_findings", "interproc_findings"} {
		if _, present := off[bucket]; present {
			t.Fatalf("%s present when gate off", bucket)
		}
	}

	on, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath (on) error = %v", err)
	}
	if _, present := on["dataflow_functions"]; !present {
		t.Fatalf("dataflow_functions absent when gate on")
	}

	delete(on, "dataflow_functions")
	delete(on, "taint_findings")
	delete(on, "interproc_findings")
	delete(on, "dataflow_catalog_versions")
	if !reflect.DeepEqual(off, on) {
		t.Fatalf("enabling dataflow changed more than the opt-in buckets")
	}
}

// TestPythonTaintSourceToSQLSink proves the intraprocedural taint bucket reports
// a request parameter reaching cursor.execute as a TAINTED sql finding.
func TestPythonTaintSourceToSQLSink(t *testing.T) {
	got := parsePythonDataflowFixture(t, pythonDataflowFixture)
	rows, ok := got["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", got["taint_findings"])
	}
	if !hasPythonTaintFinding(rows, "view", "TAINTED", "sql") {
		t.Fatalf("expected a TAINTED sql finding in view, got %+v", rows)
	}
}

// TestPythonInterprocFindingAcrossFunctions proves the interprocedural bucket
// reports a request parameter in view reaching a cursor.execute sink in the
// run_query callee.
func TestPythonInterprocFindingAcrossFunctions(t *testing.T) {
	got := parsePythonDataflowFixture(t, pythonDataflowFixture)
	rows, ok := got["interproc_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("interproc_findings bucket missing or wrong type: %T", got["interproc_findings"])
	}
	found := false
	for _, row := range rows {
		srcFn, _ := row["source_func"].(string)
		sinkFn, _ := row["sink_func"].(string)
		sinkKind, _ := row["sink_kind"].(string)
		if strings.Contains(srcFn, "view") && strings.Contains(sinkFn, "run_query") && sinkKind == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an interprocedural view->run_query sql finding, got %+v", rows)
	}
}

// TestPythonInterprocFunctionIDsIncludeRepositoryID proves Python value-flow
// identities carry stable repository identity when emitted for durable summary
// persistence.
func TestPythonInterprocFunctionIDsIncludeRepositoryID(t *testing.T) {
	got := parsePythonDataflowFixtureWithOptions(t, pythonDataflowFixture, Options{EmitDataflow: true, RepositoryID: "repo-alpha"})
	rows, ok := got["interproc_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("interproc_findings bucket missing or wrong type: %T", got["interproc_findings"])
	}
	for _, row := range rows {
		sourceFunc, _ := row["source_func"].(string)
		sinkFunc, _ := row["sink_func"].(string)
		if !strings.HasPrefix(sourceFunc, "repo-alpha\x1f") || !strings.HasPrefix(sinkFunc, "repo-alpha\x1f") {
			t.Fatalf("interproc FunctionIDs must include repo-alpha, got %+v", row)
		}
	}
}

// parsePythonDataflowFixture writes a Python fixture and parses it with the
// value-flow gate enabled.
func parsePythonDataflowFixture(t *testing.T, src string) map[string]any {
	t.Helper()
	return parsePythonDataflowFixtureWithOptions(t, src, Options{EmitDataflow: true})
}

func parsePythonDataflowFixtureWithOptions(t *testing.T, src string, options Options) map[string]any {
	t.Helper()
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "views.py")
	writeTestFile(t, filePath, src)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, options)
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	return got
}

// hasPythonTaintFinding reports whether a taint row exists for the function with
// the given kind and sink kind.
func hasPythonTaintFinding(rows []map[string]any, function, kind, sinkKind string) bool {
	for _, row := range rows {
		fn, _ := row["function_name"].(string)
		k, _ := row["kind"].(string)
		sk, _ := row["sink_kind"].(string)
		if fn == function && k == kind && sk == sinkKind {
			return true
		}
	}
	return false
}
