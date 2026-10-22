// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestDeltaSuccessorWithHeldBoundIssuesNoAnchorProbe proves the common case is
// free: a delta that deletes two files of a 100-file anchor holds the
// zero-query bound, so the anchor's file paths are never read, and the
// successor stores and enqueues projector work.
func TestDeltaSuccessorWithHeldBoundIssuesNoAnchorProbe(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 500, 800, 100)
	stream := guardStream(generation, true,
		[]string{"a/one.go", "a/two.go", "a/three.go"}, []string{"gone/x.go", "gone/y.go"},
		[]string{"a/one.go", "a/two.go", "a/three.go"}, []string{"gone/x.go", "gone/y.go"}, 3)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countQueries(successorGuardAnchorFilePathsQuery); got != 0 {
		t.Fatalf("anchor file-path probes = %d, want 0 when the bound holds", got)
	}
	requireStored(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleFileUnionBound, telemetry.SuccessorGuardOutcomeStored,
	})
}

// TestDeltaSuccessorWithFailedBoundProbesOnceAndStoresNeverIndexedDeletes
// proves the exact probe: deleting six paths of a 10-file anchor fails the
// bound, one probe runs with the scope and anchor ids, and because four of the
// six deleted paths were never indexed the union keeps 8 of 10 files and the
// successor stores.
func TestDeltaSuccessorWithFailedBoundProbesOnceAndStoresNeverIndexedDeletes(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 100, 150, 10)
	deleted := []string{"d1.go", "d2.go", "d3.go", "d4.go", "d5.go", "d6.go"}
	// The anchor holds d1, d2 plus eight untouched files; d3..d6 never existed.
	database.anchorPaths = append([]string{"d1.go", "d2.go"}, numberedPaths("keep", 8)...)
	stream := guardStream(generation, true, nil, deleted, nil, deleted, 3)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countQueries(successorGuardAnchorFilePathsQuery); got != 1 {
		t.Fatalf("anchor file-path probes = %d, want exactly 1", got)
	}
	for _, call := range database.queries {
		if call.query == successorGuardAnchorFilePathsQuery {
			if len(call.args) != 2 || call.args[0] != "scope-8750" || call.args[1] != "gen-full" {
				t.Fatalf("probe args = %v, want [scope-8750 gen-full]", call.args)
			}
		}
	}
	requireStored(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleFileUnionExact, telemetry.SuccessorGuardOutcomeStored,
	})
}

// TestDeltaSuccessorThatDeletesMostAnchorFilesIsRefusedAfterOneProbe is the
// guard's reason to exist: six real deletions out of a 10-file anchor leave a
// union of 4 files. The commit rolls back, enqueues nothing, records the
// destructive-successor skip, and counts a refused file_union_exact decision.
func TestDeltaSuccessorThatDeletesMostAnchorFilesIsRefusedAfterOneProbe(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 100, 150, 10)
	deleted := []string{"d1.go", "d2.go", "d3.go", "d4.go", "d5.go", "d6.go"}
	database.anchorPaths = append(append([]string{}, deleted...), numberedPaths("keep", 4)...)
	stream := guardStream(generation, true, nil, deleted, nil, deleted, 3)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil: a refusal is a handled outcome", err)
	}
	if got := database.countQueries(successorGuardAnchorFilePathsQuery); got != 1 {
		t.Fatalf("anchor file-path probes = %d, want exactly 1", got)
	}
	requireRefused(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleFileUnionExact, telemetry.SuccessorGuardOutcomeRefused,
	})
}

// TestDeltaSuccessorProbeFailureFailsClosed proves a probe error is not
// swallowed into a store: the commit returns the error and lands nothing.
func TestDeltaSuccessorProbeFailureFailsClosed(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 100, 150, 10)
	database.probeErr = errors.New("probe outage")
	deleted := []string{"d1.go", "d2.go", "d3.go", "d4.go", "d5.go", "d6.go"}
	stream := guardStream(generation, true, nil, deleted, nil, deleted, 3)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err == nil || !strings.Contains(err.Error(), "probe outage") {
		t.Fatalf("CommitScopeGeneration() error = %v, want the probe outage", err)
	}
	if database.tx.committed {
		t.Fatal("transaction committed despite a failed guard probe")
	}
	if len(decisions) != 0 {
		t.Fatalf("guard decisions = %v, want none for an undecided commit", decisions)
	}
}

// TestDeltaSuccessorWithoutAnchorStoresForTheProjectorToRefuse proves a delta
// whose baseline commit has no projected full generation is stored with the
// anchor_missing rule: the projector's base-missing refusal arms the reindex
// watermark, which a store-side refusal would not.
func TestDeltaSuccessorWithoutAnchorStoresForTheProjectorToRefuse(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 500, 800, 100)
	delete(database.bases, "commit-base")
	stream := guardStream(generation, true, []string{"a.go"}, nil, []string{"a.go"}, nil, 1)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countQueries(successorGuardAnchorFilePathsQuery); got != 0 {
		t.Fatalf("anchor file-path probes = %d, want 0", got)
	}
	requireStored(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleAnchorMissing, telemetry.SuccessorGuardOutcomeStored,
	})
}

// TestDeltaSuccessorAnchorDistinctFromPriorIsCountedSeparately proves the
// anchor is the base full generation, not the prior: the active prior is itself
// a delta over "gen-base", so the guard reads the anchor's file count from
// gen-base and the bound holds against it.
func TestDeltaSuccessorAnchorDistinctFromPriorIsCountedSeparately(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 0, 0, 0)
	database.candidate = []any{"gen-delta", "hint-prior", "commit-prior", "active", false, false}
	database.targets = map[string][]any{"gen-delta": {true, "commit-base"}}
	database.bases = map[string][]any{"commit-base": {"gen-base", "active"}}
	database.counts = map[string][]any{"gen-delta": {5, 5, 1}, "gen-base": {400, 400, 80}}
	stream := guardStream(generation, true, []string{"a.go"}, nil, []string{"a.go"}, nil, 1)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	counted := map[string]int{}
	for _, call := range database.queries {
		if call.query == successorGuardCountQuery {
			counted[fmt.Sprint(call.args[1])]++
		}
	}
	if counted["gen-base"] < 1 {
		t.Fatalf("count queries by generation = %v, want the anchor gen-base counted", counted)
	}
	requireStored(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleFileUnionBound, telemetry.SuccessorGuardOutcomeStored,
	})
}

// TestDeltaSuccessorWithoutDeltaRepositoryFactIsSizedAsFull proves a generation
// that claims IsDelta but whose stream carries no delta_generation fact is
// sized as a full against the prior's collector rows, the stricter rule: 41
// facts over 1000 collector rows is refused, not waved through as a union.
func TestDeltaSuccessorWithoutDeltaRepositoryFactIsSizedAsFull(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 1000, 1500, 100)
	stream := guardStream(generation, false, nil, nil, numberedPaths("f", 41), nil, 0)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countQueries(successorGuardAnchorFilePathsQuery); got != 0 {
		t.Fatalf("anchor file-path probes = %d, want 0", got)
	}
	requireRefused(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleCollectorRatio, telemetry.SuccessorGuardOutcomeRefused,
	})
}

// TestFullSuccessorIsSizedOnCollectorRowsNotReducerRows proves the like-for-like
// prior at the store seam: 300 total rows of which 100 are collector rows. A
// 60-fact full successor is 0.6 of the collector rows and stores; against the
// raw total it would be refused.
func TestFullSuccessorIsSizedOnCollectorRowsNotReducerRows(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newGuardRoutingDB(t, 100, 300, 20)
	stream := guardStream(generation, false, nil, nil, nil, nil, 60)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	requireStored(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindFull, telemetry.SuccessorGuardRuleCollectorRatio, telemetry.SuccessorGuardOutcomeStored,
	})
}

// TestFullSuccessorBelowHalfTheCollectorRowsIsRefusedAndCounted pins the
// refusal side of the same seam, with the decision counted as full /
// collector_ratio / refused.
func TestFullSuccessorBelowHalfTheCollectorRowsIsRefusedAndCounted(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newGuardRoutingDB(t, 100, 300, 20)
	stream := guardStream(generation, false, nil, nil, nil, nil, 49)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	requireRefused(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindFull, telemetry.SuccessorGuardRuleCollectorRatio, telemetry.SuccessorGuardOutcomeRefused,
	})
}

// TestDeltaSuccessorProbeReadsTheAnchorNotThePrior proves the probe targets the
// base full generation: the active prior is a delta over "gen-base" whose
// 10 files the successor mostly deletes. Reading the prior instead of the
// anchor would see a handful of delta files and miss the mass deletion.
func TestDeltaSuccessorProbeReadsTheAnchorNotThePrior(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 0, 0, 0)
	database.candidate = []any{"gen-delta", "hint-prior", "commit-prior", "active", false, false}
	database.targets = map[string][]any{"gen-delta": {true, "commit-base"}}
	database.bases = map[string][]any{"commit-base": {"gen-base", "active"}}
	database.counts = map[string][]any{"gen-delta": {5, 5, 1}, "gen-base": {100, 100, 10}}
	deleted := []string{"d1.go", "d2.go", "d3.go", "d4.go", "d5.go", "d6.go"}
	database.anchorPaths = append(append([]string{}, deleted...), numberedPaths("keep", 4)...)
	stream := guardStream(generation, true, nil, deleted, nil, deleted, 3)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	probed := ""
	for _, call := range database.queries {
		if call.query == successorGuardAnchorFilePathsQuery {
			probed = fmt.Sprint(call.args[1])
		}
	}
	if probed != "gen-base" {
		t.Fatalf("probe generation = %q, want the anchor gen-base", probed)
	}
	requireRefused(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleFileUnionExact, telemetry.SuccessorGuardOutcomeRefused,
	})
}

// TestDeltaSuccessorRenameNetsToZeroAfterOneProbe proves live files count in
// the union: renaming 50 of a 90-file anchor deletes 50 paths and adds 50 new
// ones. The bound fails (40 of 90 files) so one probe runs, and because the
// union keeps 40 untouched files plus the 50 renamed ones it holds all 90 and
// stores. Dropping the live count would read the union as 40 and refuse.
func TestDeltaSuccessorRenameNetsToZeroAfterOneProbe(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 900, 1200, 90)
	oldPaths := numberedPaths("old", 50)
	newPaths := numberedPaths("new", 50)
	database.anchorPaths = append(append([]string{}, oldPaths...), numberedPaths("keep", 40)...)
	stream := guardStream(generation, true, newPaths, oldPaths, newPaths, oldPaths, 5)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countQueries(successorGuardAnchorFilePathsQuery); got != 1 {
		t.Fatalf("anchor file-path probes = %d, want exactly 1", got)
	}
	requireStored(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleFileUnionExact, telemetry.SuccessorGuardOutcomeStored,
	})
}

// TestDeltaSuccessorSmallRenameHoldsTheBoundWithoutAProbe proves the live
// count also lifts the zero-query bound: renaming 5 of 10 files touches 10
// paths but adds 5 live ones, so the bound is 5 of 10 and no probe runs.
func TestDeltaSuccessorSmallRenameHoldsTheBoundWithoutAProbe(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(true)
	database := newGuardRoutingDB(t, 100, 100, 10)
	oldPaths := numberedPaths("old", 5)
	newPaths := numberedPaths("new", 5)
	stream := guardStream(generation, true, newPaths, oldPaths, newPaths, oldPaths, 2)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countQueries(successorGuardAnchorFilePathsQuery); got != 0 {
		t.Fatalf("anchor file-path probes = %d, want 0", got)
	}
	requireStored(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleFileUnionBound, telemetry.SuccessorGuardOutcomeStored,
	})
}

// TestSuccessorObserverAcceptsLegacyRepositoryFactKind keeps the observer in
// step with facts.ParseDeltaScope, which normalizes the legacy
// "repositoryFact" kind. Matching the kind literally would read such a delta
// as a stream/generation mismatch and size it as a full.
func TestSuccessorObserverAcceptsLegacyRepositoryFactKind(t *testing.T) {
	for _, kind := range []string{"repository", "repositoryFact"} {
		observer := newSuccessorObserver(scope.ScopeGeneration{IsDelta: true})
		observer.observe(facts.Envelope{FactKind: kind, Payload: map[string]any{
			"path": "/repos/proof", "delta_generation": true,
			"delta_relative_paths": []any{"a.go"},
		}})
		if !observer.scope.IsDelta {
			t.Fatalf("kind %q: observer scope IsDelta = false, want true", kind)
		}
	}
}
