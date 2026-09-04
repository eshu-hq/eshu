// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_language_imports_grant

// What the pinned NornicDB build itself does, measured while proving the #5167
// batch 2a grant statements. Neither test here is about the grant: one records
// a shipped builder that answers nothing on this backend for a reason unrelated
// to the grant, the other records that the backend cannot report a query plan,
// which is why the grant proof next door uses an observable ordering control
// instead. Read language_query_grant_nornicdb_live_test.go's header first.
package query

import (
	"context"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveNornicDBLanguageQueryDirectoryBuilderReturnsNothing is the negative
// control for the backend defect buildDirectoryCypher was rewritten around.
//
// The name is kept from when the shipped builder was the thing returning
// nothing. It no longer is: the builder emits one MATCH clause now and
// TestLiveNornicDBLanguageQueryGrantBindsEveryBuilder proves it answers, with
// the grant bound, alongside the other three. What still returns nothing is the
// SHAPE it used to have, and that is what this pins.
//
// On the pinned build a read with two MATCH clauses followed by a
// `WITH <node>, <node>, count(...)` aggregation drops every row as soon as the
// RETURN projects anything richer than a plain property reference or a literal.
// A function call does it -- `labels()` and `coalesce()` alike -- and so does a
// list construction, whether the list is a literal or built from a property. A
// plain property, a null property and a string literal are all fine. Collapsing
// the two clauses into one linear pattern fixes every case.
//
// No CI job builds this tag, so this control only fires when someone runs it.
// Run it against the pin before reshaping any aggregating read in this package,
// and again after moving the pin: a failure here means the backend behaviour
// the Directory rewrite was built around has changed, and the shape should be
// re-measured rather than quietly reverted.
func TestLiveNornicDBLanguageQueryDirectoryBuilderReturnsNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveGrantGraph(ctx, t, driver)

	// The shipped builder, which must now answer. If this ever returns nothing
	// again, the rewrite has been undone or the backend has changed under it.
	cypher, params := buildLanguageCypherWithSemanticFilter(
		liveGrantLanguage, "Directory", "", "", 3, "", "", liveGrantAccess(),
	)
	rows := runLiveGrantStatement(ctx, t, driver, "buildDirectoryCypher shipped", cypher, params)
	if len(rows) == 0 {
		t.Fatal("buildDirectoryCypher returned nothing; the single-clause rewrite is the whole reason this route answers on NornicDB")
	}
	// Presence is not enough. The forward single-clause rewrite
	// `(r:Repository)-[:REPO_CONTAINS|CONTAINS*]->(d:Directory)-[:CONTAINS]->(f:File)`
	// also returns rows on this build, but WRONG ones: it folds the nested
	// directory's file into the parent's file_count and drops the nested
	// directory from the answer entirely. Only the counts catch that, so the
	// granted repository's two directories are named and counted here.
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[StringVal(row, "name")] = IntVal(row, "file_count")
	}
	for name, want := range map[string]int{"z-src-0": 1, "nested": 1} {
		got, ok := counts[name]
		if !ok {
			t.Fatalf("buildDirectoryCypher lost directory %q: %#v; a rewrite that stops walking the depth-N CONTAINS chain looks like this", name, counts)
		}
		if got != want {
			t.Fatalf("buildDirectoryCypher counted %d file(s) in %q, want %d: %#v; a rewrite that folds a nested directory's file into its parent looks like this", got, name, want, counts)
		}
	}

	twoClause := `MATCH (d:Directory)<-[:CONTAINS]-(r:Repository)
MATCH (d)-[:CONTAINS]->(f:File)
WITH d, r, count(f) as c
`
	oneClause := `MATCH (f:File)<-[:CONTAINS]-(d:Directory)<-[:CONTAINS]-(r:Repository)
WITH d, r, count(f) as c
`
	for _, probe := range []struct {
		name     string
		cypher   string
		wantRows bool
	}{
		{name: "two clauses, plain properties", cypher: twoClause + `RETURN d.name as name, r.id as rid, c`, wantRows: true},
		{name: "two clauses, a property that is null", cypher: twoClause + `RETURN d.name as name, d.id as entity_id, c`, wantRows: true},
		{name: "two clauses, a string literal", cypher: twoClause + `RETURN d.name as name, 'Directory' as labels, c`, wantRows: true},
		{name: "two clauses, labels()", cypher: twoClause + `RETURN d.name as name, labels(d) as labels, c`},
		{name: "two clauses, coalesce()", cypher: twoClause + `RETURN d.name as name, coalesce(d.id, d.path) as x, c`},
		{name: "two clauses, a list literal", cypher: twoClause + `RETURN d.name as name, ['Directory'] as labels, c`},
		{name: "two clauses, a list built from a property", cypher: twoClause + `RETURN d.name as name, [d.name] as labels, c`},
		{name: "one clause, labels()", cypher: oneClause + `RETURN d.name as name, labels(d) as labels, c`, wantRows: true},
		{name: "one clause, a list literal", cypher: oneClause + `RETURN d.name as name, ['Directory'] as labels, c`, wantRows: true},
	} {
		rows := runLiveGrantStatement(ctx, t, driver, "directory bisection: "+probe.name, probe.cypher, nil)
		if probe.wantRows && len(rows) == 0 {
			t.Fatalf("directory bisection %q returned nothing, so the cause recorded in the pitfalls doc is wrong; re-measure before trusting it", probe.name)
		}
		if !probe.wantRows && len(rows) != 0 {
			t.Fatalf("directory bisection %q returned %d row(s); the pinned build's two-clause projection defect has changed, so re-measure it before relying on either shape", probe.name, len(rows))
		}
	}
}

// TestLiveNornicDBGrantPlanShapeIsNotReportable records what the pinned build
// answers when asked for a plan, so the evidence doc's claim about plan shape
// is measured rather than assumed.
//
// On sha256:4dfa887d… both EXPLAIN and PROFILE are ACCEPTED -- no syntax error
// -- and return zero rows, and the result summary carries neither a plan nor a
// profile. So the backend cannot report plan shape at all, and worse, a PROFILE
// prefix silently turns a statement that returns rows into one that returns
// none. That is why the squeeze control above stands in for a plan read, and it
// is a reason never to leave a PROFILE prefix in a shipped statement.
func TestLiveNornicDBGrantPlanShapeIsNotReportable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveGrantGraph(ctx, t, driver)

	cypher, params := buildLanguageCypherWithSemanticFilter(
		liveGrantLanguage, "File", "", "", 2, "", "", liveGrantAccess(),
	)
	plain := runLiveGrantStatement(ctx, t, driver, "plan probe plain", cypher, params)
	if len(plain) == 0 {
		t.Fatal("plan probe plain: the shipped statement returned nothing, so the comparison below is meaningless")
	}
	for _, prefix := range []string{"EXPLAIN", "PROFILE"} {
		_, prefixParams := buildLanguageCypherWithSemanticFilter(
			liveGrantLanguage, "File", "", "", 2, "", "", liveGrantAccess(),
		)
		rows := runLiveGrantStatement(ctx, t, driver, "plan probe "+prefix, prefix+" "+cypher, prefixParams)
		if len(rows) != 0 {
			t.Fatalf("plan probe %s: returned %d row(s); the pinned build used to answer nothing, so plan reporting may now exist -- re-measure and update the evidence doc", prefix, len(rows))
		}
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, "PROFILE "+cypher, params)
	if err != nil {
		t.Fatalf("plan probe summary: %v", err)
	}
	if _, err := result.Collect(ctx); err != nil {
		t.Fatalf("plan probe summary collect: %v", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		t.Fatalf("plan probe summary consume: %v", err)
	}
	if summary.Plan() != nil || summary.Profile() != nil {
		t.Fatalf("the pinned build now reports a plan (%v) or profile (%v); read it and replace the squeeze stand-in with a real plan assertion", summary.Plan(), summary.Profile())
	}
}
