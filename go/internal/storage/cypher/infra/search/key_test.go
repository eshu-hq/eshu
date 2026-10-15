// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// TestClauseShape pins the properties of the clause text the
// writers append: it reads only the node's own properties (never a
// parameter), guards every part with a non-null string type check (Neo4j
// treats null IS :: STRING as true), bounds the key at
// graph.InfraSearchKeyMaxChars, and writes both properties.
func TestClauseShape(t *testing.T) {
	t.Parallel()

	clause := Clause("r")
	if !strings.HasPrefix(clause, "SET r.infra_search_ovf = CASE WHEN size(") {
		t.Fatalf("clause = %q, want a SET of r.infra_search_ovf first", clause)
	}
	// The bound is strict: a key of exactly 8,000 code points is stored
	// (flag 'F'); 8,001 overflows (flag 'T', key null). The live test
	// TestLiveInfraSearchKeyCapBoundary proves the values on the pinned Neo4j.
	if !strings.Contains(clause, ") > 8000 THEN 'T' ELSE 'F' END, r.infra_search_key = CASE WHEN size(") ||
		!strings.Contains(clause, ") > 8000 THEN null ELSE ") {
		t.Fatalf("clause = %q, want flag 'T'/'F' and a null key past a strict 8000 bound", clause)
	}
	if !strings.Contains(clause, ", r.infra_search_key = CASE WHEN size(") {
		t.Fatalf("clause = %q, want r.infra_search_key bounded by size()", clause)
	}
	if strings.Contains(clause, "$") {
		t.Fatalf("clause = %q reads a parameter; the key must come from the node's own properties", clause)
	}
	if strings.Count(clause, "> 8000") != 2 {
		t.Fatalf("clause = %q, want the 8000 bound on both the flag and the key", clause)
	}
	for _, expr := range []string{
		"r.name", "r.id", "r.kind", "coalesce(r.resource_type, r.data_type)", "r.arn",
		"r.resource_id", "r.service_kind", "r.account_id", "r.region", "r.source", "r.config_path",
	} {
		part := "CASE WHEN " + expr + " IS :: STRING NOT NULL THEN " + expr + " ELSE '' END"
		if got := strings.Count(clause, part); got != 3 {
			t.Errorf("clause carries part %q %d times, want 3 (flag size, key size, key value)", part, got)
		}
	}
	if strings.Contains(clause, " WHERE ") || strings.Contains(clause, " WITH ") {
		t.Fatalf("clause = %q carries a clause keyword; it must stay one SET the index-key guard can read", clause)
	}
}

// TestInfraSearchKeyStatementDialect proves the dialect decides at
// construction: Neo4j appends the clause to infra-label statements, NornicDB
// returns the statement byte for byte, and a non-infra label is never keyed.
func TestInfraSearchKeyStatementDialect(t *testing.T) {
	t.Parallel()

	const base = "UNWIND $rows AS row\nMERGE (r:CloudResource {uid: row.uid})\nSET r.name = row.name"
	if got := Statement(Off, "CloudResource", "r", base); got != base {
		t.Fatalf("NornicDB statement = %q, want the base statement unchanged", got)
	}
	want := base + "\n" + Clause("r")
	if got := Statement(On, "CloudResource", "r", base); got != want {
		t.Fatalf("Neo4j statement = %q, want base plus clause", got)
	}
	if got := Statement(On, "Function", "r", base); got != base {
		t.Fatalf("non-infra label statement = %q, want unchanged", got)
	}
}

// TestDialectForBackend maps the configured graph backend, the
// same value the schema dialect split reads, to the writer dialect, and
// rejects anything else.
func TestDialectForBackend(t *testing.T) {
	t.Parallel()

	for backend, want := range map[graph.SchemaBackend]Dialect{
		graph.SchemaBackendNeo4j:    On,
		graph.SchemaBackendNornicDB: Off,
	} {
		got, err := DialectForBackend(backend)
		if err != nil || got != want {
			t.Fatalf("DialectForBackend(%q) = %v, %v; want %v", backend, got, err, want)
		}
	}
	for _, bad := range []graph.SchemaBackend{"", "memgraph"} {
		if _, err := DialectForBackend(bad); err == nil {
			t.Fatalf("DialectForBackend(%q) error = nil, want an error", bad)
		}
	}
	var zero Dialect
	if zero.Valid() {
		t.Fatal("zero Dialect is valid; it must not be a usable default")
	}
}
