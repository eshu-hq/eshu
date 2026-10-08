// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var (
	summaryNow    = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	summaryWindow = 48 * time.Hour
	// summarySince is when a confirmed row entered its state: two cycles,
	// one hour apart, ending at summaryNow.
	summarySince = summaryNow.Add(-time.Hour)
)

// confirmedRow is a live row that has held state for two cycles spanning an
// hour, so it passes Confirmed unless state is selected.
func confirmedRow(selectorID string, state State) Observation {
	return Observation{
		SelectorID:      selectorID,
		State:           state,
		LastListedAt:    summarySince.Add(-time.Hour),
		StateSince:      summarySince,
		StateCycleCount: 2,
		EvaluatedAt:     summaryNow,
		LivenessWindow:  summaryWindow,
	}
}

// pendingRow is a live row on the first evaluation in its state.
func pendingRow(selectorID string, state State) Observation {
	row := confirmedRow(selectorID, state)
	row.StateSince, row.StateCycleCount = summaryNow, 1
	return row
}

// expiredRow is a confirmed row whose selector stopped evaluating 50h ago,
// past the 48h window.
func expiredRow(selectorID string, state State) Observation {
	row := confirmedRow(selectorID, state)
	row.EvaluatedAt = summaryNow.Add(-50 * time.Hour)
	row.StateSince = row.EvaluatedAt.Add(-time.Hour)
	return row
}

// generationAt returns a latest-generation reader that reports at and counts
// its calls.
func generationAt(at time.Time, calls *int) func() (time.Time, error) {
	return func() (time.Time, error) {
		*calls++
		return at, nil
	}
}

func TestSummarizePredicateTable(t *testing.T) {
	t.Parallel()

	beforeExclusion := summarySince.Add(-time.Minute)
	tests := []struct {
		name       string
		rows       []Observation
		generation time.Time
		wantState  Aggregate
		wantReason State
		wantG      bool
	}{
		{name: "no rows is unknown", wantState: AggregateUnknown},
		{
			name:      "expired rows are unknown",
			rows:      []Observation{expiredRow("sel-a", StateNotListed), expiredRow("sel-b", StateSelected)},
			wantState: AggregateUnknown,
		},
		{name: "live selected", rows: []Observation{confirmedRow("sel-a", StateSelected)}, wantState: AggregateSelected},
		{
			name:      "explicit selected beside an org rule exclusion is selected",
			rows:      []Observation{confirmedRow("explicit-sel", StateSelected), confirmedRow("org-sel", StateRuleExcluded)},
			wantState: AggregateSelected,
		},
		{
			name: "two principals: any live selected wins",
			rows: []Observation{
				confirmedRow("rules@app:1:2", StateNotListed),
				pendingRow("rules@token:0123456789abcdef", StateSelected),
			},
			wantState: AggregateSelected,
		},
		{
			name:       "an expired selected row does not rescue a live exclusion",
			rows:       []Observation{expiredRow("sel-a", StateSelected), confirmedRow("sel-b", StateArchivedExcluded)},
			generation: beforeExclusion,
			wantState:  AggregateNotSelected,
			wantReason: StateArchivedExcluded,
			wantG:      true,
		},
		{
			name:       "first not_listed evaluation is pending confirmation",
			rows:       []Observation{pendingRow("sel-a", StateNotListed)},
			wantState:  AggregatePendingConfirmation,
			wantReason: StateNotListed,
		},
		{
			name:       "first archived evaluation is pending confirmation",
			rows:       []Observation{pendingRow("sel-a", StateArchivedExcluded)},
			wantState:  AggregatePendingConfirmation,
			wantReason: StateArchivedExcluded,
		},
		{
			name:       "one unconfirmed selector keeps the scope pending",
			rows:       []Observation{confirmedRow("sel-a", StateNotListed), pendingRow("sel-b", StateRuleExcluded)},
			wantState:  AggregatePendingConfirmation,
			wantReason: StateNotListed,
		},
		{
			name:       "confirmed not_listed with older generations is not selected",
			rows:       []Observation{confirmedRow("sel-a", StateNotListed)},
			generation: beforeExclusion,
			wantState:  AggregateNotSelected,
			wantReason: StateNotListed,
			wantG:      true,
		},
		{
			name:       "generation observed exactly at state_since is not selected",
			rows:       []Observation{confirmedRow("sel-a", StateRuleExcluded)},
			generation: summarySince,
			wantState:  AggregateNotSelected,
			wantReason: StateRuleExcluded,
			wantG:      true,
		},
		{
			name:       "generation observed after state_since is excluded but still ingested",
			rows:       []Observation{confirmedRow("sel-a", StateNotListed)},
			generation: summarySince.Add(time.Microsecond),
			wantState:  AggregateExcludedStillIngested,
			wantReason: StateNotListed,
			wantG:      true,
		},
		{
			name:       "a scope with no generation at all is not selected",
			rows:       []Observation{confirmedRow("sel-a", StateArchivedExcluded)},
			wantState:  AggregateNotSelected,
			wantReason: StateArchivedExcluded,
			wantG:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			summary, err := Summarize(tt.rows, summaryNow, generationAt(tt.generation, &calls))
			if err != nil {
				t.Fatalf("Summarize() error = %v", err)
			}
			if summary.State != tt.wantState || summary.Reason != tt.wantReason {
				t.Fatalf("Summarize() = {%s %s}, want {%s %s}", summary.State, summary.Reason, tt.wantState, tt.wantReason)
			}
			if gotG := calls > 0; gotG != tt.wantG || calls > 1 {
				t.Fatalf("latest generation read %d times, want read=%v (only after rules a-c hold, at most once)", calls, tt.wantG)
			}
		})
	}
}

// TestSummarizeQAShape is the #7625 QA corpus: 25 scopes that dropped out of
// the acme listing (24 transferred plus the renamed repository's old
// name) and archived-service, listed but archived. After two cycles every
// one of the 26 renders not_selected, because none of them has a generation
// observed after its exclusion started.
func TestSummarizeQAShape(t *testing.T) {
	t.Parallel()

	scopes := make(map[string]Observation, 26)
	for i := range 25 {
		scopes[fmt.Sprintf("acme/dropped-%02d", i)] = confirmedRow("sel-acme", StateNotListed)
	}
	scopes["acme/archived-service"] = confirmedRow("sel-acme", StateArchivedExcluded)

	counts := map[Aggregate]int{}
	for slug, row := range scopes {
		calls := 0
		summary, err := Summarize([]Observation{row}, summaryNow, generationAt(summarySince.Add(-24*time.Hour), &calls))
		if err != nil {
			t.Fatalf("%s: Summarize() error = %v", slug, err)
		}
		counts[summary.State]++
		if summary.Reason != row.State {
			t.Fatalf("%s: reason = %s, want %s", slug, summary.Reason, row.State)
		}
	}
	if counts[AggregateNotSelected] != 26 || len(counts) != 1 {
		t.Fatalf("QA classifications = %v, want all 26 not_selected", counts)
	}
}

func TestSummarizeFields(t *testing.T) {
	t.Parallel()

	t.Run("unknown carries no evidence", func(t *testing.T) {
		t.Parallel()
		summary, err := Summarize([]Observation{expiredRow("sel-a", StateNotListed)}, summaryNow, nil)
		if err != nil {
			t.Fatalf("Summarize() error = %v", err)
		}
		if summary != (Summary{State: AggregateUnknown}) {
			t.Fatalf("Summarize() = %+v, want only state unknown", summary)
		}
	})

	t.Run("selected reports the longest live selection", func(t *testing.T) {
		t.Parallel()
		older := confirmedRow("sel-a", StateSelected)
		older.StateSince = summaryNow.Add(-72 * time.Hour)
		older.LastListedAt = summaryNow.Add(-time.Minute)
		older.EvaluatedAt = summaryNow.Add(-time.Minute)
		newer := pendingRow("sel-b", StateSelected)
		newer.LastListedAt = summaryNow
		excluded := confirmedRow("sel-c", StateRuleExcluded)
		excluded.EvaluatedAt = summaryNow.Add(time.Minute)
		summary, err := Summarize([]Observation{older, newer, excluded}, summaryNow, nil)
		if err != nil {
			t.Fatalf("Summarize() error = %v", err)
		}
		want := Summary{
			State:             AggregateSelected,
			StateSince:        older.StateSince,
			LastListedAt:      summaryNow,
			EvaluatedAt:       summaryNow,
			LiveSelectorCount: 3,
		}
		if summary != want {
			t.Fatalf("Summarize() = %+v, want %+v", summary, want)
		}
	})

	t.Run("not selected reports the newest exclusion start across live rows", func(t *testing.T) {
		t.Parallel()
		a := confirmedRow("sel-a", StateNotListed)
		a.StateSince = summaryNow.Add(-3 * time.Hour)
		a.LastListedAt = time.Time{}
		b := confirmedRow("sel-b", StateArchivedExcluded)
		b.EvaluatedAt = summaryNow.Add(time.Minute)
		calls := 0
		summary, err := Summarize([]Observation{a, b, expiredRow("sel-c", StateSelected)}, summaryNow, generationAt(a.StateSince, &calls))
		if err != nil {
			t.Fatalf("Summarize() error = %v", err)
		}
		want := Summary{
			State:             AggregateNotSelected,
			Reason:            StateArchivedExcluded,
			StateSince:        b.StateSince,
			LastListedAt:      b.LastListedAt,
			EvaluatedAt:       b.EvaluatedAt,
			LiveSelectorCount: 2,
		}
		if summary != want {
			t.Fatalf("Summarize() = %+v, want %+v", summary, want)
		}
	})

	t.Run("reason ties go to the lowest selector id regardless of order", func(t *testing.T) {
		t.Parallel()
		x := pendingRow("sel-b", StateArchivedExcluded)
		y := pendingRow("sel-a", StateRuleExcluded)
		first, _ := Summarize([]Observation{x, y}, summaryNow, nil)
		second, _ := Summarize([]Observation{y, x}, summaryNow, nil)
		if first.Reason != StateRuleExcluded || second.Reason != StateRuleExcluded {
			t.Fatalf("tie reasons = %s/%s, want rule_excluded from sel-a in both orders", first.Reason, second.Reason)
		}
	})
}

func TestSummarizeLatestGenerationErrors(t *testing.T) {
	t.Parallel()

	rows := []Observation{confirmedRow("sel-a", StateNotListed)}
	boom := errors.New("scope_generations unavailable")
	if _, err := Summarize(rows, summaryNow, func() (time.Time, error) { return time.Time{}, boom }); !errors.Is(err, boom) {
		t.Fatalf("Summarize() error = %v, want the latest-generation error", err)
	}
	if _, err := Summarize(rows, summaryNow, nil); err == nil {
		t.Fatal("Summarize() with rules a-c holding and no generation reader = nil error, want an error")
	}
}

// TestSummarizeAcrossDowntime walks the amendment's liveness timeline: a row
// written before a 50h outage reads unknown once the 48h window passes, and
// the first evaluation after the outage both revives it and, for a scope
// still unlisted, confirms it, while the window stays 48h.
func TestSummarizeAcrossDowntime(t *testing.T) {
	t.Parallel()

	before := pendingRow("sel-a", StateNotListed)
	before.EvaluatedAt = summaryNow.Add(-50 * time.Hour)
	before.StateSince = before.EvaluatedAt
	if summary, _ := Summarize([]Observation{before}, summaryNow, nil); summary.State != AggregateUnknown {
		t.Fatalf("pre-downtime row after 50h = %s, want unknown", summary.State)
	}

	after := before
	after.EvaluatedAt = summaryNow
	after.StateCycleCount = 2
	calls := 0
	summary, err := Summarize([]Observation{after}, summaryNow, generationAt(before.StateSince.Add(-time.Hour), &calls))
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if summary.State != AggregateNotSelected || !summary.StateSince.Equal(before.StateSince) {
		t.Fatalf("after the outage = %+v, want not_selected since %v", summary, before.StateSince)
	}
	if after.LivenessWindow != summaryWindow {
		t.Fatalf("window = %v, want it unchanged at %v", after.LivenessWindow, summaryWindow)
	}
}
