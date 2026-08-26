// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// nornicdb_function_projection_live_test.go is the standing backend proof for
// issue #6262: the pinned NornicDB build must evaluate OPTIONAL MATCH
// projections instead of returning their literal source text.
//
// NornicDB v1.1.11 exposed two distinct corruption shapes that Eshu still
// avoids in its production query builders:
//
//   - a relationship-bound MATCH followed by an OPTIONAL MATCH corrupts every
//     function-call projection — type(rel), coalesce(...), head(labels(...));
//   - a SECOND chained OPTIONAL MATCH, matching on a variable the first bound,
//     corrupts even a PLAIN property read on its own variable — with no
//     relationship bound anywhere in the query.
//
// NornicDB PR #265 fixed both shapes. These tests now hold the corrected
// backend contract so a future regression cannot return nil, an empty value, or
// the old expression placeholder while the replay tier still reports green.
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

// TestNornicDBFunctionProjectionEvaluatesAfterOptionalMatch requires the
// corrected traversal-seeded OPTIONAL MATCH evaluator shipped in NornicDB
// v1.2.3.
func TestNornicDBFunctionProjectionEvaluatesAfterOptionalMatch(t *testing.T) {
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

	// This is the historical corruption shape: a relationship-bound MATCH
	// followed by OPTIONAL MATCH, with function calls in the RETURN.
	withOptional, err := exec.Run(ctx, `MATCH (source:`+p+`Fn)-[rel:IMPORTS]->(target:`+p+`Mod)
OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:`+p+`File)<-[:REPO_CONTAINS]-(sourceRepo:`+p+`Repo)
RETURN type(rel) AS type,
       coalesce(source.id, source.uid) AS source_id,
       head(labels(source)) AS source_type,
       source.name AS source_name,
       sourceFile.relative_path AS source_file_path`, nil)
	if err != nil {
		t.Fatalf("optional-match read: %v", err)
	}
	if len(withOptional) != 1 {
		t.Fatalf("optional-match rows = %d, want 1: %+v", len(withOptional), withOptional)
	}
	t.Logf("with OPTIONAL MATCH: %+v", withOptional[0])

	for field, want := range map[string]any{
		"type":             "IMPORTS",
		"source_id":        "fn-a",
		"source_type":      functionProjectionLabelPrefix + "Fn",
		"source_name":      "handler",
		"source_file_path": "app.ts",
	} {
		if got := withOptional[0][field]; got != want {
			t.Errorf("%s = %#v, want %#v; nil, empty, or literal-expression values are backend regressions", field, got, want)
		}
	}
}

// TestNornicDBSecondChainedOptionalMatchEvaluatesPlainPropertyReads requires a
// second chained OPTIONAL MATCH to bind and project its own variables.
//
// Older builds returned "sourceRepo.id" for the second-hop property, including
// when no relationship variable appeared in the query. Exact repo-1 assertions
// below reject that placeholder as well as nil or empty results.
func TestNornicDBSecondChainedOptionalMatchEvaluatesPlainPropertyReads(t *testing.T) {
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

	// Every binding, including the second OPTIONAL MATCH, must survive.
	for field, want := range map[string]string{
		"type":             "IMPORTS",
		"source_legacy_id": "fn-a",
		"source_uid":       "fn-a",
		"source_name":      "handler",
		"source_file_path": "app.ts",
		"source_repo_id":   "repo-1",
	} {
		if got := rows[0][field]; got != want {
			t.Errorf("%s = %#v, want %q; nil, empty, or literal-expression values are backend regressions", field, got, want)
		}
	}

	// The node-only variant proves the fixed behavior does not depend on a
	// relationship variable being bound by the primary MATCH.
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
	for field, want := range map[string]string{
		"source_file_path": "app.ts",
		"source_repo_id":   "repo-1",
	} {
		if got := nodeOnly[0][field]; got != want {
			t.Errorf("node-only %s = %#v, want %q; nil, empty, or literal-expression values are backend regressions", field, got, want)
		}
	}
}
