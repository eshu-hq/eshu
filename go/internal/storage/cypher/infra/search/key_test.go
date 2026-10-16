// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"regexp"
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
	// The joined key is computed once, into a row variable, so no over-cap
	// value is ever written to the node; both properties are SET from it.
	if want := "WITH r, " + joinedExpr("r") + " AS infra_search_joined_key\nSET r.infra_search_ovf = "; !strings.HasPrefix(clause, want) {
		t.Fatalf("clause = %q, want prefix %q", clause, want)
	}
	// The bound is strict: a key of exactly 8,000 code points is stored
	// (flag 'F'); 8,001 overflows (flag 'T', key null). The live test
	// TestLiveInfraSearchKey proves the values on the pinned Neo4j.
	wantTail := "SET r.infra_search_ovf = CASE WHEN size(infra_search_joined_key) > 8000 THEN 'T' ELSE 'F' END, " +
		"r.infra_search_key = CASE WHEN size(infra_search_joined_key) > 8000 THEN null ELSE infra_search_joined_key END"
	if !strings.HasSuffix(clause, wantTail) {
		t.Fatalf("clause = %q, want suffix %q", clause, wantTail)
	}
	if strings.Contains(clause, "$") {
		t.Fatalf("clause = %q reads a parameter; the key must come from the node's own properties", clause)
	}
	if got := strings.Count(clause, "IS :: STRING NOT NULL"); got != 11 {
		t.Fatalf("clause evaluates %d guarded parts, want the 11 parts once each", got)
	}
	if strings.Contains(clause, " WHERE ") || strings.Count(clause, "WITH ") != 1 {
		t.Fatalf("clause = %q, want exactly one WITH and no WHERE", clause)
	}
}

// TestJoinedExprReadsExactlyFields keeps the expression the clause evaluates
// equal to Fields(): every property it reads is a searched field, every
// searched field is read, each part is guarded as a non-null string, and the
// resource type is the coalesce the predicate reads, in that order.
func TestJoinedExprReadsExactlyFields(t *testing.T) {
	t.Parallel()

	expr := joinedExpr("n")
	read := map[string]bool{}
	for _, m := range regexp.MustCompile(`\bn\.(\w+)\b`).FindAllStringSubmatch(expr, -1) {
		read[m[1]] = true
	}
	want := map[string]bool{}
	for _, f := range Fields() {
		want[f] = true
	}
	if len(read) != len(want) {
		t.Fatalf("joinedExpr reads %v, want exactly Fields() %v", read, want)
	}
	for f := range want {
		if !read[f] {
			t.Errorf("joinedExpr does not read %s", f)
		}
	}
	parts := strings.Split(expr, " + '\\u001F' + ")
	if len(parts) != 11 {
		t.Fatalf("joinedExpr has %d parts, want 11", len(parts))
	}
	for _, part := range parts {
		if !strings.HasPrefix(part, "CASE WHEN ") || !strings.Contains(part, " IS :: STRING NOT NULL THEN ") || !strings.HasSuffix(part, " ELSE '' END") {
			t.Errorf("part %q is not a guarded string part", part)
		}
	}
	if !strings.Contains(expr, "coalesce(n.resource_type, n.data_type)") {
		t.Fatal("joinedExpr does not coalesce resource_type before data_type")
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
