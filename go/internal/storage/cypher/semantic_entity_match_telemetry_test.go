// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
)

// TestVerifySemanticEntityMatchIdentitiesFlagsDivergentCanonicalRows is the
// seeded RED for the #8359 MATCH-miss telemetry: a canonical-owned row whose
// uid is not the projector's derivation can never MATCH on NornicDB, so it
// must surface as unmatched (attempted=1, matched=0) with its uid named.
// A row carrying the exact derivation is the matched case and stays silent.
func TestVerifySemanticEntityMatchIdentitiesFlagsDivergentCanonicalRows(t *testing.T) {
	t.Parallel()

	const repoID = "repo-ifa-semantic-entity-containment"
	divergent := semantic.EntityRow{
		RepoID: repoID, EntityID: "content-entity:sec-py-func",
		EntityType: "Function", EntityName: "fetch",
		FilePath:     "/repo-semantic-containment/app/fetch.py",
		RelativePath: "app/fetch.py", Language: "python",
		StartLine: 3, EndLine: 9, Metadata: map[string]any{"async": true},
	}
	matchedUID := content.CanonicalEntityID(repoID, "app/fetch.py", "Function", "fetch", 3)
	agreed := divergent
	agreed.EntityID = matchedUID

	attempted, matched, unmatched := verifySemanticEntityMatchIdentities(
		[]semantic.EntityRow{divergent, agreed}, true,
	)
	if attempted["Function"] != 2 {
		t.Errorf("attempted[Function] = %d, want 2", attempted["Function"])
	}
	if matched["Function"] != 1 {
		t.Errorf("matched[Function] = %d, want 1", matched["Function"])
	}
	if len(unmatched) != 1 || unmatched[0] != "content-entity:sec-py-func" {
		t.Errorf("unmatched = %v, want [content-entity:sec-py-func]", unmatched)
	}
}

// TestVerifySemanticEntityMatchIdentitiesTrustsMergeModeAndPreservedTypes
// pins the ambiguous cases: node-creating write-mode labels land by
// construction whatever uid they carry, and a MATCH-owned label the projector
// does NOT re-derive (Typedef keeps its incoming uid) still MATCHes a
// divergent uid. Neither may report unmatched.
func TestVerifySemanticEntityMatchIdentitiesTrustsMergeModeAndPreservedTypes(t *testing.T) {
	t.Parallel()

	rows := []semantic.EntityRow{
		{
			RepoID: "repo-1", EntityID: "content-entity:sec-ex-var",
			EntityType: "Variable", EntityName: "@timeout",
			FilePath:     "/repo/lib/worker.ex",
			RelativePath: "lib/worker.ex", Language: "elixir",
			StartLine: 4, EndLine: 4,
		},
		{
			RepoID: "repo-1", EntityID: "content-entity:custom-typedef",
			EntityType: "Typedef", EntityName: "Handler",
			FilePath:     "/repo/pkg/handle.go",
			RelativePath: "pkg/handle.go", Language: "go",
			StartLine: 5, EndLine: 8,
		},
	}
	attempted, matched, unmatched := verifySemanticEntityMatchIdentities(rows, true)
	if attempted["Variable"] != 1 || matched["Variable"] != 1 {
		t.Errorf("Variable attempted/matched = %d/%d, want 1/1", attempted["Variable"], matched["Variable"])
	}
	if attempted["Typedef"] != 1 || matched["Typedef"] != 1 {
		t.Errorf("Typedef attempted/matched = %d/%d, want 1/1", attempted["Typedef"], matched["Typedef"])
	}
	if len(unmatched) != 0 {
		t.Errorf("unmatched = %v, want empty", unmatched)
	}

	// Legacy (node-creating) write mode never MATCHes: even a divergent
	// Function uid lands, so it stays silent there too.
	legacy := semantic.EntityRow{
		RepoID: "repo-1", EntityID: "content-entity:sec-py-func",
		EntityType: "Function", EntityName: "fetch",
		FilePath:     "/repo/app/fetch.py",
		RelativePath: "app/fetch.py", Language: "python",
		StartLine: 3, EndLine: 9,
	}
	attempted, matched, unmatched = verifySemanticEntityMatchIdentities([]semantic.EntityRow{legacy}, false)
	if attempted["Function"] != 1 || matched["Function"] != 1 {
		t.Errorf("legacy Function attempted/matched = %d/%d, want 1/1", attempted["Function"], matched["Function"])
	}
	if len(unmatched) != 0 {
		t.Errorf("legacy unmatched = %v, want empty", unmatched)
	}
}

// TestVerifySemanticEntityMatchIdentitiesCountsMalformedRowsUnmatched pins
// that a row the writer cannot even build a statement for (no start line)
// counts as attempted but unmatched: admitted rows that reach no statement
// are the same accuracy defect class as a MATCH-miss.
func TestVerifySemanticEntityMatchIdentitiesCountsMalformedRowsUnmatched(t *testing.T) {
	t.Parallel()

	row := semantic.EntityRow{
		RepoID: "repo-1", EntityID: "content-entity:sec-bad",
		EntityType: "Function", EntityName: "bad",
		FilePath:     "/repo/app/bad.py",
		RelativePath: "app/bad.py", Language: "python",
		StartLine: 0, EndLine: 0,
	}
	attempted, matched, unmatched := verifySemanticEntityMatchIdentities([]semantic.EntityRow{row}, true)
	if attempted["Function"] != 1 {
		t.Errorf("attempted[Function] = %d, want 1", attempted["Function"])
	}
	if matched["Function"] != 0 {
		t.Errorf("matched[Function] = %d, want 0", matched["Function"])
	}
	if len(unmatched) != 1 || unmatched[0] != "content-entity:sec-bad" {
		t.Errorf("unmatched = %v, want [content-entity:sec-bad]", unmatched)
	}
}
