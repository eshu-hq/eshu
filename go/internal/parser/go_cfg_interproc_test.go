// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGoInterprocFindingAcrossFunctions proves the value-flow engine detects an
// interprocedural taint flow in real Go: an *http.Request parameter in handle is
// passed to query, whose parameter reaches a SQL sink.
func TestGoInterprocFindingAcrossFunctions(t *testing.T) {
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
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}

	rows, ok := got["interproc_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("interproc_findings bucket missing or wrong type: %T", got["interproc_findings"])
	}
	found := false
	for _, row := range rows {
		srcFn, _ := row["source_func"].(string)
		sinkFn, _ := row["sink_func"].(string)
		sinkKind, _ := row["sink_kind"].(string)
		if strings.Contains(srcFn, "handle") && strings.Contains(sinkFn, "query") && sinkKind == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an interprocedural handle->query sql finding, got %+v", rows)
	}
}

// TestGoInterprocFunctionIDsIncludeRepositoryID proves persisted value-flow
// identities are keyed by stable repository identity, not a blank repo slot.
func TestGoInterprocFunctionIDsIncludeRepositoryID(t *testing.T) {
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
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true, RepositoryID: "repo-alpha"})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}

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

// TestGoInterprocNoFalseEdgeFromMethodCall proves a method call (conn.Query)
// whose field name matches a local function (Query) does NOT resolve to that
// local function, so no false cross-function finding is produced from the caller.
func TestGoInterprocNoFalseEdgeFromMethodCall(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

import (
	"database/sql"
	"net/http"
)

func Query(userControlled *http.Request, db *sql.DB) {
	db.Query(userControlled.FormValue("x"))
}

func handle(req *http.Request, conn *sql.DB) {
	conn.Query(req)
}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	rows, _ := got["interproc_findings"].([]map[string]any)
	for _, row := range rows {
		srcFn, _ := row["source_func"].(string)
		sinkFn, _ := row["sink_func"].(string)
		// handle's own conn.Query is a real sql sink, so a handle->handle finding
		// is legitimate. The false edge would be a cross-function handle->Query.
		if strings.Contains(srcFn, "handle") && srcFn != sinkFn {
			t.Fatalf("false cross-function finding from handle via method call conn.Query: %+v", row)
		}
	}
}

// TestGoInterprocOffIsByteIdentical proves the interproc findings bucket is absent
// when the gate is off.
func TestGoInterprocOffIsByteIdentical(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

import "net/http"

func handle(r *http.Request) {
	_ = r.FormValue("q")
}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	off, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath (off) error = %v", err)
	}
	if _, present := off["interproc_findings"]; present {
		t.Fatalf("interproc_findings present when gate off")
	}
}

// TestGoInterprocNoFalseEdgeFromShadowedCallee proves that a call whose name is
// shadowed by a parameter (a function value) is not resolved to a same-named
// package function, so no false cross-function finding is produced.
func TestGoInterprocNoFalseEdgeFromShadowedCallee(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

import (
	"net/http"
	"os/exec"
)

func query(r *http.Request) {
	exec.Command(r.FormValue("c"))
}

func handle(userReq *http.Request, query func(*http.Request)) {
	query(userReq)
}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	rows, _ := got["interproc_findings"].([]map[string]any)
	for _, row := range rows {
		srcFn, _ := row["source_func"].(string)
		sinkFn, _ := row["sink_func"].(string)
		if strings.Contains(srcFn, "handle") && srcFn != sinkFn {
			t.Fatalf("false cross-function finding from handle via shadowed callee query: %+v", row)
		}
	}
}

// TestGoInterprocCallBeforeLocalShadow proves a bare call that precedes a local
// binding of the same name still resolves to the package function (the local
// declared later does not shadow the earlier call), so the finding is kept.
func TestGoInterprocCallBeforeLocalShadow(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

import (
	"database/sql"
	"net/http"
)

func sink(r *http.Request, db *sql.DB) {
	db.Query(r.FormValue("q"))
}

func handle(r *http.Request, db *sql.DB) {
	sink(r, db)
	sink := r
	_ = sink
}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	rows, _ := got["interproc_findings"].([]map[string]any)
	found := false
	for _, row := range rows {
		srcFn, _ := row["source_func"].(string)
		sinkFn, _ := row["sink_func"].(string)
		sinkKind, _ := row["sink_kind"].(string)
		if strings.Contains(srcFn, "handle") && strings.Contains(sinkFn, "sink") && sinkKind == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected handle->sink finding (call precedes the local shadow); got %+v", rows)
	}
}
