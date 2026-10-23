// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package workload_test

import (
	"context"
	"testing"

	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
)

// TestLiveDetectClassifiesSeededCases is the #8795 positive/negative/ambiguous
// proof of the shared detection path on the Neo4j the DSN points at.
// It seeds one fixture per retirement case (nonce-scoped, so it is safe on a
// shared backend) and asserts the verdict of each through the same Detect call
// the readiness check, the dry run and the execute verification use.
func TestLiveDetectClassifiesSeededCases(t *testing.T) {
	db := openLiveDB(t)
	f := seedFixture(t, db)

	report, err := legacyworkload.Detect(context.Background(), db, legacyworkload.Options{
		IDs: append(f.legacyIDs(), f.scoped("b", "n1")), // N1 must never become a candidate
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]legacyworkload.Verdict{}
	for _, v := range report.Verdicts {
		got[v.ID] = v
	}
	if _, found := got[f.scoped("b", "n1")]; found {
		t.Fatal("N1: a scoped node must never be a candidate, even with a null generation_id")
	}
	want := map[string]struct {
		reason string
		rels   map[string]int
	}{
		"p1":  {legacyworkload.BlockedNone, map[string]int{}},
		"n2":  {legacyworkload.BlockedHasInstances, map[string]int{}},
		"n2b": {legacyworkload.BlockedHasInstances, map[string]int{}},
		"n3":  {legacyworkload.BlockedNoTwin, map[string]int{}},
		"n4":  {legacyworkload.BlockedNoTwin, map[string]int{}},
		"n5":  {legacyworkload.BlockedRelationships, map[string]int{"DOCUMENTS": 1}},
		"n5b": {legacyworkload.BlockedRelationships, map[string]int{"DEPENDS_ON": 1}},
		"n6":  {legacyworkload.BlockedNoTwin, map[string]int{}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d verdicts, want %d: %+v", len(got), len(want), got)
	}
	for name, w := range want {
		v, ok := got[f.legacy(name)]
		if !ok {
			t.Errorf("%s: no verdict", name)
			continue
		}
		if v.BlockedReason != w.reason || v.Eligible != (w.reason == legacyworkload.BlockedNone) {
			t.Errorf("%s: eligible=%v reason=%q, want reason %q", name, v.Eligible, v.BlockedReason, w.reason)
		}
		for typ, n := range w.rels {
			if v.Relationships[typ] != n {
				t.Errorf("%s: relationships[%s] = %d, want %d (all: %v)", name, typ, v.Relationships[typ], n, v.Relationships)
			}
		}
		if len(v.Relationships) != len(w.rels) {
			t.Errorf("%s: relationships = %v, want %v", name, v.Relationships, w.rels)
		}
	}

	// The unscoped scan must find the same set (plus anything else on a shared
	// backend), so the label-wide read agrees with the id-anchored one.
	full, err := legacyworkload.Detect(context.Background(), db, legacyworkload.Options{MaxExamined: 5000, PageSize: 7})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, v := range full.Verdicts {
		seen[v.ID] = true
	}
	for _, name := range caseNames {
		if !seen[f.legacy(name)] {
			t.Errorf("full scan missed %s", name)
		}
	}
	if seen[f.scoped("b", "n1")] {
		t.Error("full scan returned the scoped null-generation node")
	}
}

// TestLiveDetectTwoScopedNodesSameNameIsNotACandidate is the A1 ambiguous
// case: two scoped nodes with one name in different repositories are
// legitimately ambiguous to the resolver and are never a leftover.
func TestLiveDetectTwoScopedNodesSameNameIsNotACandidate(t *testing.T) {
	db := openLiveDB(t)
	f := seedFixture(t, db)
	for _, r := range []string{"a1", "a2"} {
		db.mustWrite(t, `CREATE (:Repository {id: $id})`, map[string]any{"id": f.repo(r)})
		db.mustWrite(t, `CREATE (:Workload {id: $id, name: $name, repo_id: $repo, generation_id: 'g1'})`,
			map[string]any{"id": f.scoped(r, "dup"), "name": "dup-" + f.nonce, "repo": f.repo(r)})
	}
	report, err := legacyworkload.Detect(context.Background(), db, legacyworkload.Options{
		IDs: []string{f.scoped("a1", "dup"), f.scoped("a2", "dup"), "workload:dup-" + f.nonce},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Verdicts) != 0 {
		t.Fatalf("verdicts = %+v, want none: two scoped same-name nodes are not leftovers", report.Verdicts)
	}
}
