// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// nornicdb_function_projection_live_test.go is the standing backend proof for
// issue #5694: on the pinned NornicDB build, an OPTIONAL MATCH can make a
// RETURN column evaluate to its own literal source text instead of a value.
//
// Two distinct shapes trigger it, and the second is wider than what
// nornicdb-pitfalls.md originally recorded:
//
//   - a relationship-bound MATCH followed by an OPTIONAL MATCH corrupts every
//     function-call projection — type(rel), coalesce(...), head(labels(...));
//   - a SECOND chained OPTIONAL MATCH, matching on a variable the first bound,
//     corrupts even a PLAIN property read on its own variable — with no
//     relationship bound anywhere in the query.
//
// The query builders already avoid the shape (see
// docs/public/reference/nornicdb-pitfalls.md, "Trailing OPTIONAL MATCH Corrupts
// Every Function-Call Projection"), and unit tests assert the emitted Cypher
// text. What no test did was hold the BACKEND behavior those rewrites exist
// for. That gap matters in both directions:
//
//   - if a future build fixes the defect, this test starts failing and the
//     rewrites can be reconsidered instead of being carried forever as
//     unexplained complexity;
//   - if someone reintroduces the shape, a text assertion catches it only where
//     a guard happens to exist, while this proves what the backend does with it.
//
// Issue #5691 made this sharper: File-[:IMPORTS]->Module edges now exist, so a
// relationship read that corrupts its type column finally has real data to
// corrupt.
//
// Skills active: golang-engineering, cypher-query-rigor,
// eshu-diagnostic-rigor.

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const functionProjectionLabelPrefix = "Probe5694"

func functionProjectionCleanup(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, stmt := range []string{
		`MATCH (n:` + functionProjectionLabelPrefix + `Fn) DETACH DELETE n`,
		`MATCH (n:` + functionProjectionLabelPrefix + `Mod) DETACH DELETE n`,
		`MATCH (n:` + functionProjectionLabelPrefix + `File) DETACH DELETE n`,
		`MATCH (n:` + functionProjectionLabelPrefix + `Repo) DETACH DELETE n`,
	} {
		if err := exec.Execute(ctx, cypher.Statement{Cypher: stmt}); err != nil {
			t.Fatalf("cleanup %q: %v", stmt, err)
		}
	}
}

// functionProjectionSeed builds a File->Function containment chain under a
// repository, plus one IMPORTS edge, using probe-only labels so the fixture
// cannot collide with another live-tier test on a shared backend.
func functionProjectionSeed(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	p := functionProjectionLabelPrefix
	for _, stmt := range []string{
		`MERGE (f:` + p + `Fn {uid: 'fn-a', id: 'fn-a', name: 'handler', language: 'typescript'})`,
		`MERGE (m:` + p + `Mod {uid: 'mod-b', id: 'mod-b', name: 'express'})`,
		`MERGE (file:` + p + `File {path: '/r/app.ts', relative_path: 'app.ts', language: 'typescript'})`,
		`MERGE (r:` + p + `Repo {id: 'repo-1', name: 'app'})`,
		`MATCH (f:` + p + `Fn {uid:'fn-a'}) MATCH (m:` + p + `Mod {uid:'mod-b'}) MERGE (f)-[:IMPORTS]->(m)`,
		`MATCH (file:` + p + `File) MATCH (f:` + p + `Fn {uid:'fn-a'}) MERGE (file)-[:CONTAINS]->(f)`,
		`MATCH (r:` + p + `Repo) MATCH (file:` + p + `File) MERGE (r)-[:REPO_CONTAINS]->(file)`,
	} {
		if err := exec.Execute(ctx, cypher.Statement{Cypher: stmt}); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
}

// TestNornicDBFunctionProjectionCorruptsAfterOptionalMatch pins the defect
// itself. It asserts the CURRENT backend behavior, corruption included, so the
// rewrites elsewhere in the query layer have a stated reason a reader can
// verify rather than a comment they have to trust.
//
// If this test fails because the projections evaluated correctly, that is good
// news, not a regression: the pinned build changed, and
// docs/public/reference/nornicdb-pitfalls.md plus the rewrites that cite it
// should be revisited together.
func TestNornicDBFunctionProjectionCorruptsAfterOptionalMatch(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the function-projection proof against a real NornicDB", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	functionProjectionCleanup(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		functionProjectionCleanup(cleanCtx, t, exec)
	})
	functionProjectionSeed(ctx, t, exec)

	p := functionProjectionLabelPrefix

	// Baseline: the same projections, with no OPTIONAL MATCH anywhere. These
	// evaluate correctly, which is what makes the contrast below a defect in
	// the OPTIONAL MATCH path rather than a missing feature.
	baseline, err := exec.Run(ctx, `MATCH (source:`+p+`Fn)-[rel:IMPORTS]->(target:`+p+`Mod)
RETURN type(rel) AS type,
       coalesce(source.id, source.uid) AS source_id,
       head(labels(source)) AS source_type`, nil)
	if err != nil {
		t.Fatalf("baseline read: %v", err)
	}
	if len(baseline) != 1 {
		t.Fatalf("baseline rows = %d, want 1: %+v", len(baseline), baseline)
	}
	t.Logf("no OPTIONAL MATCH: %+v", baseline[0])
	if got := baseline[0]["type"]; got != "IMPORTS" {
		t.Fatalf("baseline type = %v, want IMPORTS — the projection is broken even without an OPTIONAL MATCH", got)
	}
	if got := baseline[0]["source_id"]; got != "fn-a" {
		t.Fatalf("baseline source_id = %v, want fn-a", got)
	}

	// The shape the query layer must not emit: a relationship-bound MATCH
	// followed by chained OPTIONAL MATCHes, with function calls in the RETURN.
	corrupted, err := exec.Run(ctx, `MATCH (source:`+p+`Fn)-[rel:IMPORTS]->(target:`+p+`Mod)
OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:`+p+`File)<-[:REPO_CONTAINS]-(sourceRepo:`+p+`Repo)
RETURN type(rel) AS type,
       coalesce(source.id, source.uid) AS source_id,
       source.name AS source_name,
       sourceFile.relative_path AS source_file_path`, nil)
	if err != nil {
		t.Fatalf("optional-match read: %v", err)
	}
	if len(corrupted) != 1 {
		t.Fatalf("optional-match rows = %d, want 1: %+v", len(corrupted), corrupted)
	}
	t.Logf("with OPTIONAL MATCH: %+v", corrupted[0])

	// Plain property reads still work. Only the function calls corrupt, which
	// is why this is so easy to miss: the row looks populated.
	if got := corrupted[0]["source_name"]; got != "handler" {
		t.Errorf("source_name = %v, want handler — plain property reads are expected to survive", got)
	}
	if got := corrupted[0]["source_file_path"]; got != "app.ts" {
		t.Errorf("source_file_path = %v, want app.ts — the OPTIONAL MATCH itself does bind", got)
	}

	if got := corrupted[0]["type"]; got != "type(rel)" {
		t.Errorf("type = %v; the pinned build is expected to return the literal source text %q. "+
			"If it now returns IMPORTS, the backend defect is fixed: revisit nornicdb-pitfalls.md "+
			"and the query-layer rewrites that cite it", got, "type(rel)")
	}
	if got := corrupted[0]["source_id"]; got != "coalesce(source.id, source.uid)" {
		t.Errorf("source_id = %v; the pinned build is expected to return the literal source text", got)
	}
}

// TestNornicDBSecondChainedOptionalMatchCorruptsPlainPropertyReads records a
// boundary wider than the one nornicdb-pitfalls.md documented.
//
// The documented shape is "relationship-bound MATCH + OPTIONAL MATCH corrupts
// function-call projections". Measuring it here shows the relationship binding
// is not required and function calls are not required either: with two chained
// OPTIONAL MATCHes, where the second matches on a variable the first bound, a
// PLAIN property read on the second one's variable returns its own source text.
// Reading the first one's variable in the same query is fine.
//
// This is why go/internal/query/code_relationship_story_nornicdb.go pairs every
// second-hop column with its literal placeholder through
// nornicDBStoryProjection, and collapses any value equal to that placeholder:
// the query layer cannot avoid the second hop, so it detects the corruption
// instead. This test is the backend half of that contract.
func TestNornicDBSecondChainedOptionalMatchCorruptsPlainPropertyReads(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the function-projection proof against a real NornicDB", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	functionProjectionCleanup(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		functionProjectionCleanup(cleanCtx, t, exec)
	})
	functionProjectionSeed(ctx, t, exec)

	p := functionProjectionLabelPrefix
	rows, err := exec.Run(ctx, `MATCH (source:`+p+`Fn)-[rel:IMPORTS]->(target:`+p+`Mod)
OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:`+p+`File)
OPTIONAL MATCH (sourceRepo:`+p+`Repo)-[:REPO_CONTAINS]->(sourceFile)
RETURN 'IMPORTS' AS type,
       source.id AS source_legacy_id,
       source.uid AS source_uid,
       source.name AS source_name,
       sourceFile.relative_path AS source_file_path,
       sourceRepo.id AS source_repo_id`, nil)
	if err != nil {
		t.Fatalf("rewrite read: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rewrite rows = %d, want 1: %+v", len(rows), rows)
	}
	t.Logf("rewritten shape: %+v", rows[0])

	// Everything bound by the first MATCH or the first OPTIONAL MATCH survives.
	for field, want := range map[string]string{
		"type":             "IMPORTS",
		"source_legacy_id": "fn-a",
		"source_uid":       "fn-a",
		"source_name":      "handler",
		"source_file_path": "app.ts",
	} {
		if got := rows[0][field]; got != want {
			t.Errorf("%s = %v, want %v — first-hop reads are expected to survive", field, got, want)
		}
	}

	// The second chained OPTIONAL MATCH does not. This is a plain property
	// read, not a function call, which is the part the pitfall page understated.
	// The variant the pitfalls table records but a relationship-bound query
	// cannot demonstrate: no relationship anywhere, same corruption. Without
	// this the doc claims a boundary the regression does not hold.
	nodeOnly, err := exec.Run(ctx, `MATCH (source:`+p+`Fn)
OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:`+p+`File)
OPTIONAL MATCH (sourceRepo:`+p+`Repo)-[:REPO_CONTAINS]->(sourceFile)
RETURN sourceFile.relative_path AS source_file_path,
       sourceRepo.id AS source_repo_id`, nil)
	if err != nil {
		t.Fatalf("node-only read: %v", err)
	}
	if len(nodeOnly) != 1 {
		t.Fatalf("node-only rows = %d, want 1: %+v", len(nodeOnly), nodeOnly)
	}
	t.Logf("no relationship bound: %+v", nodeOnly[0])
	if got := nodeOnly[0]["source_file_path"]; got != "app.ts" {
		t.Errorf("node-only source_file_path = %v, want app.ts — the first hop still binds", got)
	}
	if got := nodeOnly[0]["source_repo_id"]; got != "sourceRepo.id" {
		t.Errorf("node-only source_repo_id = %v, want the literal %q: the corruption does not need a relationship bound anywhere, "+
			"which is the part nornicdb-pitfalls.md originally understated", got, "sourceRepo.id")
	}

	if got := rows[0]["source_repo_id"]; got != "sourceRepo.id" {
		t.Errorf("source_repo_id = %v; the pinned build is expected to return the literal %q. "+
			"If it now returns repo-1, the defect is narrower than recorded: revisit "+
			"nornicdb-pitfalls.md and the nornicDBStoryProjection placeholder collapse "+
			"in code_relationship_story_nornicdb.go, which exists only because of this", got, "sourceRepo.id")
	}
}
