// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The tests in this file pin the #8750 review F1 ruling: the guard's prior is
// the larger of the scope's active generation and the duplicate-check
// candidate, the pending side counts only while its projector work is live,
// and a failed active read fails the commit closed.

// newPendingWindowDB stages the F1 window: the candidate is a pending delta
// "gen-c" whose base is missing (sized on its own 11 rows, #8362), and the
// active generation is the full "gen-full" with 400 collector rows and 200
// files at commit-base.
func newPendingWindowDB(t *testing.T) *guardRoutingDB {
	t.Helper()
	return &guardRoutingDB{
		t: t, tx: &fakeTx{}, logs: &bytes.Buffer{},
		candidate: []any{"gen-c", "hint-c", "commit-c", "pending", false, true},
		activeID:  "gen-full",
		targets:   map[string][]any{"gen-c": {true, "commit-b"}, "gen-full": {false, ""}},
		bases:     map[string][]any{"commit-base": {"gen-full", "active"}},
		counts:    map[string][]any{"gen-c": {11, 11, 5}, "gen-full": {400, 700, 200}},
	}
}

// countedGenerations returns how many successorGuardCountQuery reads ran per
// generation id.
func (g *guardRoutingDB) countedGenerations() map[string]int {
	counted := map[string]int{}
	for _, call := range g.queries {
		if call.query == successorGuardCountQuery {
			counted[fmt.Sprint(call.args[1])]++
		}
	}
	return counted
}

// stageLog returns the first JSON log record for the commit stage.
func (g *guardRoutingDB) stageLog(t *testing.T, stage string) map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(g.logs.Bytes()))
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode log line %q: %v", scanner.Text(), err)
		}
		if record["stage"] == stage {
			return record
		}
	}
	t.Fatalf("no %q stage log in:\n%s", stage, g.logs.String())
	return nil
}

// H1: a pending orphan delta candidate and a different active full. Both are
// counted, the larger (active) decides, and the log names the active side.
func TestGuardPriorTakesTheActiveGenerationWhenItIsLarger(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newPendingWindowDB(t)
	stream := guardStream(generation, false, nil, nil, nil, nil, 40)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	counted := database.countedGenerations()
	if counted["gen-c"] < 1 || counted["gen-full"] < 1 {
		t.Fatalf("count queries by generation = %v, want both gen-c and gen-full counted", counted)
	}
	requireRefused(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindFull, telemetry.SuccessorGuardRuleCollectorRatio, telemetry.SuccessorGuardOutcomeRefused,
	})
	record := database.stageLog(t, "successor_guard")
	if record["prior_sizing"] != "active" || record["prior_sizing_generation_id"] != "gen-full" {
		t.Fatalf("prior_sizing = %v (%v), want active (gen-full)", record["prior_sizing"], record["prior_sizing_generation_id"])
	}
	if record["prior_active_collector_fact_count"] != float64(400) || record["prior_candidate_collector_fact_count"] != float64(11) {
		t.Fatalf("side counts = active %v, candidate %v, want 400 and 11",
			record["prior_active_collector_fact_count"], record["prior_candidate_collector_fact_count"])
	}
}

// H2: an active candidate is the active generation; the hot path adds no
// active lookup and counts once.
func TestGuardPriorIssuesNoActiveLookupWhenTheCandidateIsActive(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newGuardRoutingDB(t, 100, 300, 20)
	stream := guardStream(generation, false, nil, nil, nil, nil, 60)

	if _, err := commitGuardScenario(t, database, generation, scopeValue, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countQueries(successorGuardActiveGenerationQuery); got != 0 {
		t.Fatalf("active lookups = %d, want 0 for an active candidate", got)
	}
	if got := database.countQueries(successorGuardCountQuery); got != 1 {
		t.Fatalf("count queries = %d, want 1", got)
	}
	requireStored(t, database)
}

// H3: the candidate activated between the candidate read and the active read,
// so the lookup returns the candidate id: one lookup, one count.
func TestGuardPriorCountsOnceWhenTheActiveIsTheCandidate(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newGuardRoutingDB(t, 0, 0, 0)
	database.candidate = []any{"gen-p", "hint-p", "commit-p", "pending", false, true}
	database.activeID = "gen-p"
	database.targets = map[string][]any{"gen-p": {false, ""}}
	database.counts = map[string][]any{"gen-p": {100, 100, 50}}
	stream := guardStream(generation, false, nil, nil, nil, nil, 60)

	if _, err := commitGuardScenario(t, database, generation, scopeValue, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if got := database.countQueries(successorGuardActiveGenerationQuery); got != 1 {
		t.Fatalf("active lookups = %d, want 1 for a pending candidate", got)
	}
	if got := database.countQueries(successorGuardCountQuery); got != 1 {
		t.Fatalf("count queries = %d, want 1 when the active is the candidate", got)
	}
	requireStored(t, database)
}

// H4: a failed active lookup or a failed active count fails the commit
// closed: the error returns, nothing is stored, nothing is refused.
func TestGuardPriorActiveReadFailureFailsClosed(t *testing.T) {
	for name, stage := range map[string]func(*guardRoutingDB){
		"lookup": func(g *guardRoutingDB) { g.activeErr = errors.New("active outage") },
		"count":  func(g *guardRoutingDB) { g.countErrs = map[string]error{"gen-full": errors.New("active outage")} },
	} {
		t.Run(name, func(t *testing.T) {
			scopeValue, generation := guardDeltaFixture(false)
			database := newPendingWindowDB(t)
			stage(database)
			stream := guardStream(generation, false, nil, nil, nil, nil, 40)

			decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
			if err == nil || !strings.Contains(err.Error(), "active outage") {
				t.Fatalf("CommitScopeGeneration() error = %v, want the active outage", err)
			}
			if database.tx.committed || database.enqueuedProjectorWork() {
				t.Fatal("a commit with an unreadable active prior stored")
			}
			if len(decisions) != 0 {
				t.Fatalf("guard decisions = %v, want none", decisions)
			}
			if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
				t.Fatalf("destructive_successor skips = %d, want 0", got)
			}
		})
	}
}

// H5: the active generation is a delta whose base is missing. It is sized on
// its own rows, logs the orphan fallback, and as the larger side it carries
// the orphan rule.
func TestGuardPriorSizesAnOrphanActiveOnItsOwnRows(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newGuardRoutingDB(t, 0, 0, 0)
	database.logs = &bytes.Buffer{}
	database.candidate = []any{"gen-p", "hint-p", "commit-p", "pending", false, true}
	database.activeID = "gen-d"
	database.targets = map[string][]any{"gen-p": {false, ""}, "gen-d": {true, "commit-x"}}
	database.counts = map[string][]any{"gen-p": {50, 50, 25}, "gen-d": {300, 300, 100}}
	stream := guardStream(generation, false, nil, nil, nil, nil, 100)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	requireRefused(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindFull, telemetry.SuccessorGuardRuleOrphanFallback, telemetry.SuccessorGuardOutcomeRefused,
	})
	if record := database.stageLog(t, "successor_prior_orphan_fallback"); record["prior_sizing_generation_id"] != "gen-d" {
		t.Fatalf("orphan fallback generation = %v, want gen-d", record["prior_sizing_generation_id"])
	}
	record := database.stageLog(t, "successor_guard")
	if record["prior_sizing"] != "own_rows_base_missing" || record["prior_sizing_generation_id"] != "gen-d" {
		t.Fatalf("prior_sizing = %v (%v), want own_rows_base_missing (gen-d)", record["prior_sizing"], record["prior_sizing_generation_id"])
	}
}

// H5, both sides empty: an empty successor still refuses when either side is
// orphan-sized (#8362), even though the larger side is a plain generation.
func TestGuardPriorKeepsTheOrphanEmptyRefusalWhenBothSidesAreEmpty(t *testing.T) {
	scopeValue, generation := guardDeltaFixture(false)
	database := newGuardRoutingDB(t, 0, 0, 0)
	database.candidate = []any{"gen-p", "hint-p", "commit-p", "pending", false, true}
	database.activeID = "gen-d"
	database.targets = map[string][]any{"gen-p": {false, ""}, "gen-d": {true, "commit-x"}}
	database.counts = map[string][]any{"gen-p": {0, 0, 0}, "gen-d": {0, 0, 0}}
	stream := guardStream(generation, false, nil, nil, nil, nil, 0)

	decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	requireRefused(t, database)
	requireDecisions(t, decisions, guardDecisionKey{
		telemetry.SuccessorGuardKindFull, telemetry.SuccessorGuardRuleOrphanFallback, telemetry.SuccessorGuardOutcomeRefused,
	})
}

// H6: a pending candidate whose projector work is not live (dead-lettered or
// wedged) cannot activate, so with an active generation it is not sized; with
// live work it is, and with no active generation it is the only prior.
func TestGuardPriorCountsAPendingCandidateOnlyWhileItsWorkIsLive(t *testing.T) {
	for _, tc := range []struct {
		name       string
		workLive   bool
		activeID   string
		wantRefuse bool
	}{
		{name: "wedged pending with an active", workLive: false, activeID: "gen-full", wantRefuse: false},
		{name: "live pending with an active", workLive: true, activeID: "gen-full", wantRefuse: true},
		{name: "wedged pending with no active", workLive: false, activeID: "", wantRefuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scopeValue, generation := guardDeltaFixture(false)
			database := newGuardRoutingDB(t, 0, 0, 0)
			database.candidate = []any{"gen-p", "hint-p", "commit-p", "pending", false, tc.workLive}
			database.activeID = tc.activeID
			database.targets = map[string][]any{"gen-p": {false, ""}, "gen-full": {false, ""}}
			database.counts = map[string][]any{"gen-p": {2000, 2000, 1000}, "gen-full": {200, 200, 100}}
			stream := guardStream(generation, false, nil, nil, nil, nil, 150)

			if _, err := commitGuardScenario(t, database, generation, scopeValue, stream); err != nil {
				t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
			}
			if tc.wantRefuse {
				requireRefused(t, database)
			} else {
				requireStored(t, database)
				if got := database.countedGenerations()["gen-p"]; got != 0 {
					t.Fatalf("wedged candidate counted %d times, want 0 next to an active", got)
				}
			}
		})
	}
}

// H7: a delta successor in the window. The max-sized prior feeds the
// no_prior and empty arms; the anchor resolution still runs once, on the
// delta's baseline commit, and no anchor probe runs when the bound holds.
func TestGuardPriorFeedsTheDeltaArmsWithoutChangingTheAnchor(t *testing.T) {
	t.Run("no_prior arm sees the active rows", func(t *testing.T) {
		scopeValue, generation := guardDeltaFixture(true)
		database := newGuardRoutingDB(t, 0, 0, 0)
		database.candidate = []any{"gen-p", "hint-p", "commit-p", "pending", false, true}
		database.activeID = "gen-full"
		database.targets = map[string][]any{"gen-p": {false, ""}, "gen-full": {false, ""}}
		database.counts = map[string][]any{"gen-p": {0, 0, 0}, "gen-full": {400, 700, 200}}
		stream := guardStream(generation, true, []string{"a.go"}, nil, []string{"a.go"}, nil, 1)

		decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
		if err != nil {
			t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
		}
		requireStored(t, database)
		requireDecisions(t, decisions, guardDecisionKey{
			telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleFileUnionBound, telemetry.SuccessorGuardOutcomeStored,
		})
		var baselines []string
		for _, call := range database.queries {
			if call.query == deltaUnionBaseQuery {
				baselines = append(baselines, fmt.Sprint(call.args[1]))
			}
		}
		if len(baselines) != 1 || baselines[0] != "commit-base" {
			t.Fatalf("anchor resolutions = %v, want exactly [commit-base]", baselines)
		}
		if got := database.countQueries(successorGuardAnchorFilePathsQuery); got != 0 {
			t.Fatalf("anchor probes = %d, want 0", got)
		}
	})
	t.Run("empty arm sees the active rows", func(t *testing.T) {
		scopeValue, generation := guardDeltaFixture(true)
		database := newGuardRoutingDB(t, 0, 0, 0)
		database.candidate = []any{"gen-p", "hint-p", "commit-p", "pending", false, true}
		database.activeID = "gen-full"
		database.targets = map[string][]any{"gen-p": {false, ""}, "gen-full": {false, ""}}
		database.counts = map[string][]any{"gen-p": {0, 0, 0}, "gen-full": {400, 700, 200}}
		// IsDelta with no delta_generation fact and no facts: sized as a full.
		stream := guardStream(generation, false, nil, nil, nil, nil, 0)

		decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
		if err != nil {
			t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
		}
		requireRefused(t, database)
		requireDecisions(t, decisions, guardDecisionKey{
			telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleEmpty, telemetry.SuccessorGuardOutcomeRefused,
		})
	})
}

// Review N1: when the candidate is pending, a delta's anchor is normally the
// active full generation, which the active side has already sized. The
// anchor must reuse that count, not read it a second time. The delta deletes
// 120 of the anchor's 200 files, so the decision needs the real anchor file
// count: a reuse that returned zero files would store a wipe.
func TestGuardPriorReusesTheActiveSideForTheAnchor(t *testing.T) {
	for _, workLive := range []bool{true, false} {
		t.Run(fmt.Sprintf("candidate work live %v", workLive), func(t *testing.T) {
			scopeValue, generation := guardDeltaFixture(true)
			database := newGuardRoutingDB(t, 0, 0, 0)
			database.candidate = []any{"gen-p", "hint-p", "commit-p", "pending", false, workLive}
			database.activeID = "gen-full"
			database.targets = map[string][]any{"gen-p": {false, ""}, "gen-full": {false, ""}}
			database.counts = map[string][]any{"gen-p": {50, 50, 20}, "gen-full": {400, 700, 200}}
			database.anchorPaths = numberedPaths("f", 200)
			deleted := numberedPaths("f", 120)
			stream := guardStream(generation, true, deleted, deleted, nil, deleted, 0)

			decisions, err := commitGuardScenario(t, database, generation, scopeValue, stream)
			if err != nil {
				t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
			}
			requireRefused(t, database)
			requireDecisions(t, decisions, guardDecisionKey{
				telemetry.SuccessorGuardKindDelta, telemetry.SuccessorGuardRuleFileUnionExact, telemetry.SuccessorGuardOutcomeRefused,
			})
			if got := database.countedGenerations()["gen-full"]; got != 1 {
				t.Fatalf("gen-full counted %d times, want once: the anchor must reuse the active side", got)
			}
		})
	}
}
