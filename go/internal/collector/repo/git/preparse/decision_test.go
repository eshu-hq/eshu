// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import (
	"errors"
	"testing"
)

func eligibleInput() Input {
	return Input{
		Reason: ReasonIntervalElapsed,
		Active: ActiveGeneration{
			Present: true, GenerationID: "gen-a", CommitSHA: "commit-a", Identity: "preparse:v1:x", Skips: 0,
		},
		RemoteCommit: "commit-a",
		Identity:     "preparse:v1:x",
		MaxSkips:     7,
	}
}

func clean() (bool, error) { return true, nil }

// TestDecideCells pins the decision table of ruling a9-143 (D1, D3, items 1
// and 7): only interval_elapsed and graph_dirty may skip, and only when the
// active generation is a full at the remote head with an equal stored
// identity, the tree is clean, there are no submodules, and the valve allows.
func TestDecideCells(t *testing.T) {
	t.Parallel()
	cells := []struct {
		name   string
		mutate func(*Input)
		clean  func() (bool, error)
		want   Outcome
	}{
		{"eligible, equal identity", nil, clean, OutcomeSkipped},
		{"graph_dirty is eligible", func(in *Input) { in.Reason = ReasonGraphDirty }, clean, OutcomeSkipped},
		{"reindex_requested parses", func(in *Input) { in.Reason = "reindex_requested" }, clean, OutcomeReasonIneligible},
		{"repository_reindex_requested parses", func(in *Input) { in.Reason = "repository_reindex_requested" }, clean, OutcomeReasonIneligible},
		{"reconcile_request_stale parses", func(in *Input) { in.Reason = "reconcile_request_stale" }, clean, OutcomeReasonIneligible},
		{"never_reconciled parses", func(in *Input) { in.Reason = "never_reconciled" }, clean, OutcomeReasonIneligible},
		{"in_flight_expired parses", func(in *Input) { in.Reason = "in_flight_expired" }, clean, OutcomeReasonIneligible},
		{"retry_after_unprojected parses", func(in *Input) { in.Reason = "retry_after_unprojected" }, clean, OutcomeReasonIneligible},
		{"valve off (0) never skips", func(in *Input) { in.MaxSkips = 0 }, clean, OutcomeDisabled},
		{"no active generation", func(in *Input) { in.Active.Present = false }, clean, OutcomeNoActiveFull},
		{"delta active", func(in *Input) { in.Active.IsDelta = true }, clean, OutcomeDeltaActive},
		{"head moved", func(in *Input) { in.RemoteCommit = "commit-b" }, clean, OutcomeHeadMoved},
		{"identity absent (first sweep after deploy)", func(in *Input) { in.Active.Identity = "" }, clean, OutcomeIdentityAbsent},
		{"identity mismatch (config or version moved)", func(in *Input) { in.Identity = "preparse:v1:y" }, clean, OutcomeIdentityMismatch},
		{"current identity unknown", func(in *Input) { in.Identity = "" }, clean, OutcomeIdentityAbsent},
		{"submodules present", func(in *Input) { in.HasSubmodules = true }, clean, OutcomeSubmodules},
		{"dirty tree", nil, func() (bool, error) { return false, nil }, OutcomeDirtyTree},
		{"status read error", nil, func() (bool, error) { return false, errors.New("git status failed") }, OutcomeReadError},
	}
	for _, cell := range cells {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()
			in := eligibleInput()
			if cell.mutate != nil {
				cell.mutate(&in)
			}
			if got := Decide(in, cell.clean); got != cell.want {
				t.Fatalf("Decide() = %q, want %q", got, cell.want)
			}
		})
	}
}

// TestDecideSkipsTheTreeCheckWhenEarlierRulesParse: the git status call is the
// only non-trivial cost, so it runs only when every cheaper rule passed.
func TestDecideSkipsTheTreeCheckWhenEarlierRulesParse(t *testing.T) {
	t.Parallel()
	in := eligibleInput()
	in.RemoteCommit = "commit-b"
	called := false
	Decide(in, func() (bool, error) { called = true; return true, nil })
	if called {
		t.Fatal("the tree check ran although the head had moved")
	}
}

// TestValveParsesOnTheNthInterval (D3): with MaxSkips N the scope skips N-1
// times after a parse, then parses on the Nth interval; N=1 never skips.
func TestValveParsesOnTheNthInterval(t *testing.T) {
	t.Parallel()
	in := eligibleInput()
	in.MaxSkips = 7
	for skips := 0; skips < 6; skips++ {
		in.Active.Skips = skips
		if got := Decide(in, clean); got != OutcomeSkipped {
			t.Fatalf("skips=%d: Decide() = %q, want skipped", skips, got)
		}
	}
	in.Active.Skips = 6
	if got := Decide(in, clean); got != OutcomeValveDue {
		t.Fatalf("skips=6 with N=7: Decide() = %q, want valve_due", got)
	}
	in.MaxSkips = 1
	in.Active.Skips = 0
	if got := Decide(in, clean); got != OutcomeValveDue {
		t.Fatalf("N=1: Decide() = %q, want valve_due (parse every interval)", got)
	}
}

// TestOutcomesAreClosed: the telemetry label set is exactly the outcomes.
func TestOutcomesAreClosed(t *testing.T) {
	t.Parallel()
	seen := map[Outcome]bool{}
	for _, outcome := range Outcomes() {
		if outcome == "" || seen[outcome] {
			t.Fatalf("outcome %q empty or repeated", outcome)
		}
		seen[outcome] = true
	}
	if len(seen) != 15 {
		t.Fatalf("outcome count = %d, want 15", len(seen))
	}
}

// TestStampWithoutParseCells (#8095): the opt-in first-sweep stamp fires only
// when the setting is on, the stored proof matches the running collector, the
// stored identity is empty while the current one is known, and every other
// skip guard (reason, active full, head, submodules, valve, clean tree) also
// allows it. Any doubt parses with the outcome the unproven path would give.
func TestStampWithoutParseCells(t *testing.T) {
	t.Parallel()
	stampable := func(in *Input) {
		in.Active.Identity = ""
		in.StampWithoutParse = true
		in.StampProven = true
	}
	cells := []struct {
		name   string
		mutate func(*Input)
		clean  func() (bool, error)
		want   Outcome
	}{
		{"off by default parses", func(in *Input) { in.Active.Identity = "" }, clean, OutcomeIdentityAbsent},
		{"opt-in without proof parses", func(in *Input) {
			in.Active.Identity = ""
			in.StampWithoutParse = true
		}, clean, OutcomeIdentityAbsent},
		{"proven without opt-in parses", func(in *Input) {
			in.Active.Identity = ""
			in.StampProven = true
		}, clean, OutcomeIdentityAbsent},
		{"proven stamp, no parse", stampable, clean, OutcomeStamped},
		{"stamp needs the current identity", func(in *Input) {
			stampable(in)
			in.Identity = ""
		}, clean, OutcomeIdentityAbsent},
		{"stamp keeps a stored identity on the skip path", func(in *Input) {
			in.StampWithoutParse = true
			in.StampProven = true
		}, clean, OutcomeSkipped},
		{"stamp refuses a moved head", func(in *Input) {
			stampable(in)
			in.RemoteCommit = "commit-b"
		}, clean, OutcomeHeadMoved},
		{"stamp refuses a delta", func(in *Input) {
			stampable(in)
			in.Active.IsDelta = true
		}, clean, OutcomeDeltaActive},
		{"stamp refuses an ineligible reason", func(in *Input) {
			stampable(in)
			in.Reason = "reindex_requested"
		}, clean, OutcomeReasonIneligible},
		{"stamp refuses submodules", func(in *Input) {
			stampable(in)
			in.HasSubmodules = true
		}, clean, OutcomeSubmodules},
		{"stamp refuses a due valve", func(in *Input) {
			stampable(in)
			in.Active.Skips = 6
		}, clean, OutcomeValveDue},
		{"stamp refuses a dirty tree", stampable, func() (bool, error) { return false, nil }, OutcomeDirtyTree},
	}
	for _, cell := range cells {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()
			in := eligibleInput()
			if cell.mutate != nil {
				cell.mutate(&in)
			}
			if got := Decide(in, cell.clean); got != cell.want {
				t.Fatalf("Decide() = %q, want %q", got, cell.want)
			}
		})
	}
}
