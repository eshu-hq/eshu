// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func conflictedPR() fx {
	f := openPR(1, "aaaa1111")
	f.mergeable, f.mergeState = "CONFLICTING", "DIRTY"
	return f
}

func TestConflictReadsTheGeneratedListFromTheCheckout(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := "GENERATED=(\n  specs/live-tests.v1.yaml\n)\n"
	if err := os.WriteFile(filepath.Join(dir, "scripts", "verify-live-postgres-readiness-drift.sh"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newWorld()
	w.seed(1, conflictedPR())
	w.mergeTree["base0001..aaaa1111"] = mergeTreeResult{out: "tree\nspecs/live-tests.v1.yaml\n", code: 1}
	h := newHarness(t, []int{1}, w)
	h.wt.cfg.Dir = dir

	got := h.poll()
	wantKinds(t, got, "BASELINE", "CONFLICT")
	if r := parseEvent(t, got[1]).kv["recipe"]; r != "4a" {
		t.Fatalf("a path in the checkout's GENERATED list must be 4a, got %q: %s", r, got[1])
	}
}

func TestConflictWithoutTheGeneratedListDoesNotClaimTheRecipe(t *testing.T) {
	w := newWorld()
	w.seed(1, conflictedPR())
	w.mergeTree["base0001..aaaa1111"] = mergeTreeResult{out: "tree\nspecs/live-tests.v1.yaml\n", code: 1}
	h := newHarness(t, []int{1}, w) // Dir does not exist: no list can be read

	got := h.poll()
	if r := parseEvent(t, got[1]).kv["recipe"]; r != "count-choice-lines" {
		t.Fatalf("without the list the watcher must not claim 4a, got %q", r)
	}
}

func TestConflictInAnAgentRuleFileIsCalledOut(t *testing.T) {
	w := newWorld()
	w.seed(1, conflictedPR())
	w.mergeTree["base0001..aaaa1111"] = mergeTreeResult{out: "tree\nAGENTS.md\nCHANGELOG.md\ngo/cmd/x/AGENTS.md\n", code: 1}
	h := newHarness(t, []int{1}, w)

	e := parseEvent(t, h.poll()[1])
	if e.kv["recipe"] != "count-choice-lines" || e.kv["agent_rule_files"] != "2" {
		t.Fatalf("agent-rule conflict misreported: %s", e.raw)
	}
}
