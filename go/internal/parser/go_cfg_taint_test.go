package parser

import (
	"path/filepath"
	"testing"
)

// taintFindingsBucket returns the taint_findings rows from a parsed payload.
func taintFindingsBucket(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	rows, ok := payload["taint_findings"].([]map[string]any)
	if !ok {
		t.Fatalf("taint_findings bucket missing or wrong type: %T", payload["taint_findings"])
	}
	return rows
}

// hasFinding reports whether a finding row exists with the given kind, sink
// kind, and binding.
func hasFinding(rows []map[string]any, kind, sinkKind, binding string) bool {
	for _, row := range rows {
		k, _ := row["kind"].(string)
		sk, _ := row["sink_kind"].(string)
		b, _ := row["binding"].(string)
		if k == kind && sk == sinkKind && b == binding {
			return true
		}
	}
	return false
}

func parseGoTaintFixture(t *testing.T, body string) map[string]any {
	t.Helper()
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handlers.go")
	writeTestFile(t, filePath, body)
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, Options{EmitDataflow: true})
	if err != nil {
		t.Fatalf("ParsePath error = %v", err)
	}
	return got
}

// TestGoTaintSourceToSQLSink proves an HTTP request value flowing into a SQL
// query without sanitization yields a TAINTED finding of kind sql.
func TestGoTaintSourceToSQLSink(t *testing.T) {
	got := parseGoTaintFixture(t, `package handlers

import (
	"database/sql"
	"net/http"
)

func handle(r *http.Request, db *sql.DB) {
	q := r.FormValue("q")
	db.Query(q)
}
`)
	rows := taintFindingsBucket(t, got)
	if !hasFinding(rows, "TAINTED", "sql", "q") {
		t.Fatalf("expected TAINTED sql finding for q, got %+v", rows)
	}
}

// TestGoTaintWrongKindSanitizerStillTainted proves an HTML escaper does not
// suppress a SQL sink (the kind-set model end-to-end through Go).
func TestGoTaintWrongKindSanitizerStillTainted(t *testing.T) {
	got := parseGoTaintFixture(t, `package handlers

import (
	"database/sql"
	"html"
	"net/http"
)

func handle(r *http.Request, db *sql.DB) {
	q := r.FormValue("q")
	e := html.EscapeString(q)
	db.Query(e)
}
`)
	rows := taintFindingsBucket(t, got)
	if !hasFinding(rows, "TAINTED", "sql", "e") {
		t.Fatalf("expected TAINTED sql finding for e (html escaper must not suppress sql), got %+v", rows)
	}
}

// TestGoTaintCorrectSanitizerSuppresses proves an HTML escaper suppresses an
// HTML sink: the finding is SANITIZES, not TAINTED.
func TestGoTaintCorrectSanitizerSuppresses(t *testing.T) {
	got := parseGoTaintFixture(t, `package handlers

import (
	"html"
	"html/template"
	"net/http"
)

func render(r *http.Request) template.HTML {
	raw := r.FormValue("name")
	safe := html.EscapeString(raw)
	return template.HTML(safe)
}
`)
	rows := taintFindingsBucket(t, got)
	if hasFinding(rows, "TAINTED", "html", "safe") {
		t.Fatalf("did not expect TAINTED html finding for safe (escaper should suppress), got %+v", rows)
	}
	if !hasFinding(rows, "SANITIZES", "html", "safe") {
		t.Fatalf("expected SANITIZES html finding for safe, got %+v", rows)
	}
}

// TestGoTaintFieldSensitiveSourceToSQLSink proves a source assigned into one
// struct field reaches a sink through that field without tainting a sibling.
func TestGoTaintFieldSensitiveSourceToSQLSink(t *testing.T) {
	got := parseGoTaintFixture(t, `package handlers

import (
	"database/sql"
	"net/http"
)

type payload struct {
	SQL     string
	Display string
}

func handle(r *http.Request, db *sql.DB) {
	var data payload
	data.SQL = r.FormValue("q")
	data.Display = "safe"
	db.Query(data.SQL)
	db.Query(data.Display)
}
`)
	rows := taintFindingsBucket(t, got)
	if !hasFinding(rows, "TAINTED", "sql", "data.SQL") {
		t.Fatalf("expected TAINTED sql finding for data.SQL, got %+v", rows)
	}
	if hasFinding(rows, "TAINTED", "sql", "data.Display") {
		t.Fatalf("did not expect sibling field data.Display to be tainted, got %+v", rows)
	}
}

// TestGoTaintPointerAliasSourceCallToSQLSink proves source-call assignments
// through a pointer alias are classified on the normalized field path.
func TestGoTaintPointerAliasSourceCallToSQLSink(t *testing.T) {
	got := parseGoTaintFixture(t, `package handlers

import (
	"database/sql"
	"os"
)

type payload struct {
	SQL string
}

func handle(db *sql.DB) {
	var data payload
	alias := &data
	alias.SQL = os.Getenv("Q")
	db.Query(data.SQL)
}
`)
	rows := taintFindingsBucket(t, got)
	if !hasFinding(rows, "TAINTED", "sql", "data.SQL") {
		t.Fatalf("expected TAINTED sql finding for data.SQL through pointer alias source call, got %+v", rows)
	}
}

// TestGoTaintPointerAliasSanitizerSuppresses proves sanitizer assignments
// through a pointer alias are classified on the normalized field path.
func TestGoTaintPointerAliasSanitizerSuppresses(t *testing.T) {
	got := parseGoTaintFixture(t, `package handlers

import (
	"html"
	"html/template"
	"net/http"
)

type payload struct {
	HTML string
}

func render(r *http.Request) template.HTML {
	var data payload
	alias := &data
	raw := r.FormValue("name")
	alias.HTML = html.EscapeString(raw)
	return template.HTML(data.HTML)
}
`)
	rows := taintFindingsBucket(t, got)
	if hasFinding(rows, "TAINTED", "html", "data.HTML") {
		t.Fatalf("did not expect TAINTED html finding for data.HTML through pointer alias sanitizer, got %+v", rows)
	}
	if !hasFinding(rows, "SANITIZES", "html", "data.HTML") {
		t.Fatalf("expected SANITIZES html finding for data.HTML through pointer alias sanitizer, got %+v", rows)
	}
}

// TestGoTaintOffIsByteIdentical proves the taint findings bucket is absent when
// the dataflow gate is off.
func TestGoTaintOffIsByteIdentical(t *testing.T) {
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
	if _, present := off["taint_findings"]; present {
		t.Fatalf("taint_findings present when gate off")
	}
}
