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

// TestLiveNornicDBLanguageQueryDirectoryBuilderReturnsNothing is the fourth
// language builder's result, and it is not a grant result.
//
// buildDirectoryCypher returns ZERO rows on the pinned build, scoped and
// unscoped alike, against a graph seeded exactly the way the canonical
// projector writes directories. So `entity_type: "directory"` on
// POST /api/v0/code/language-query answers an empty results list on the default
// graph backend for every caller. That is pre-existing and independent of this
// branch: the same statement minus the grant is equally empty.
//
// The cause is a pinned-backend defect, bisected below and recorded in
// docs/public/reference/nornicdb-query-pitfalls.md: a statement with TWO MATCH
// clauses followed by a `WITH <node>, <node>, count(...)` aggregation returns
// nothing as soon as any labels() call appears, in the RETURN or in the WITH.
// It is not the grant, not the variable-length REPO_CONTAINS|CONTAINS pattern,
// and not the language OR-chain.
//
// This test therefore pins the defect rather than the grant. It fails the day
// the backend starts answering, which is the day this builder's grant becomes
// provable the way the other three already are -- swap it back into
// TestLiveNornicDBLanguageQueryGrantBindsEveryBuilder then.
func TestLiveNornicDBLanguageQueryDirectoryBuilderReturnsNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveGrantGraph(ctx, t, driver)

	for _, access := range []struct {
		name   string
		filter repositoryAccessFilter
	}{
		{name: "scoped", filter: liveGrantAccess()},
		{name: "unscoped", filter: liveGrantUnscopedAccess()},
	} {
		cypher, params := buildLanguageCypherWithSemanticFilter(
			liveGrantLanguage, "Directory", "", "", 3, "", "", access.filter,
		)
		rows := runLiveGrantStatement(ctx, t, driver, "buildDirectoryCypher "+access.name, cypher, params)
		if len(rows) != 0 {
			t.Fatalf("buildDirectoryCypher %s returned %d row(s); the pinned build used to answer none, so the directory entity type may now work -- prove its grant alongside the other three builders and update the evidence doc: %#v",
				access.name, len(rows), rows)
		}
	}

	// The bisection, so the cause is recorded as a measurement rather than a
	// sentence. Only the labels() variants are empty.
	twoClause := `MATCH (d:Directory)<-[:CONTAINS]-(r:Repository)
MATCH (d)-[:CONTAINS]->(f:File)
`
	oneClause := `MATCH (r:Repository)-[:CONTAINS]->(d:Directory)-[:CONTAINS]->(f:File)
`
	for _, probe := range []struct {
		name     string
		cypher   string
		wantRows bool
	}{
		{name: "two MATCH, aggregation, labels() in RETURN", cypher: twoClause + `WITH d, r, count(f) as c RETURN d.name as name, labels(d) as labels, c`},
		{name: "two MATCH, aggregation, labels() computed in the WITH", cypher: twoClause + `WITH d, r, labels(d) as ls, count(f) as c RETURN d.name as name, ls as labels, c`},
		{name: "two MATCH, aggregation, no labels()", cypher: twoClause + `WITH d, r, count(f) as c RETURN d.name as name, r.id as rid, c`, wantRows: true},
		{name: "one MATCH, aggregation, labels() in RETURN", cypher: oneClause + `WITH d, r, count(f) as c RETURN d.name as name, labels(d) as labels, c`, wantRows: true},
	} {
		rows := runLiveGrantStatement(ctx, t, driver, "directory bisection: "+probe.name, probe.cypher, nil)
		if probe.wantRows && len(rows) == 0 {
			t.Fatalf("directory bisection %q returned nothing, so the cause recorded in the pitfalls doc is wrong; re-measure before trusting it", probe.name)
		}
		if !probe.wantRows && len(rows) != 0 {
			t.Fatalf("directory bisection %q returned %d row(s); the pinned build's labels()-after-aggregation defect has changed, so re-measure it", probe.name, len(rows))
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
