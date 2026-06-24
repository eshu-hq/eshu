// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestGoDataflowSourcesEmitParamEntryPoints proves the parser emits a
// dataflow_sources bucket naming each function's param-level taint entry points
// (e.g. an *http.Request parameter), which the cross-repo fixpoint needs as
// source ports.
func TestGoDataflowSourcesEmitParamEntryPoints(t *testing.T) {
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
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true, RepositoryID: "repo-alpha", GoPackageImportPath: "example.com/repo/handlers"})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	rows, ok := got["dataflow_sources"].([]map[string]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("dataflow_sources bucket missing or empty: %T", got["dataflow_sources"])
	}
	httpSource := false
	for _, row := range rows {
		id, _ := row["function_id"].(string)
		kind, _ := row["kind"].(string)
		if kind == "http_request" && id != "" {
			httpSource = true
		}
	}
	if !httpSource {
		t.Fatalf("expected an http_request source param, got %+v", rows)
	}
}

// TestGoDataflowSourcesRequireRepositoryID proves durable source rows are not
// emitted without the repository identity the persistence layer keys on.
func TestGoDataflowSourcesRequireRepositoryID(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, `package handlers

import "net/http"

func handle(r *http.Request) {}
`)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	if _, present := got["dataflow_sources"]; present {
		t.Fatalf("dataflow_sources emitted without repository id: %+v", got["dataflow_sources"])
	}
}
