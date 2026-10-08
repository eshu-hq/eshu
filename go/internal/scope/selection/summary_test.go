// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

import (
	"testing"
	"time"
)

var (
	summaryNow      = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	summaryInterval = 5 * time.Minute
)

func liveRow(selectorID string, state State) Observation {
	return Observation{
		SelectorID:         selectorID,
		State:              state,
		LastListedAt:       summaryNow,
		EvaluatedAt:        summaryNow,
		EvaluationInterval: summaryInterval,
	}
}

func unlistedRow(selectorID string, cycles int, firstUnlisted time.Time) Observation {
	return Observation{
		SelectorID:         selectorID,
		State:              StateNotListed,
		LastListedAt:       firstUnlisted.Add(-summaryInterval),
		FirstUnlistedAt:    firstUnlisted,
		UnlistedCycleCount: cycles,
		EvaluatedAt:        summaryNow,
		EvaluationInterval: summaryInterval,
	}
}

func staleRow(selectorID string, state State) Observation {
	row := liveRow(selectorID, state)
	row.EvaluatedAt = summaryNow.Add(-4 * summaryInterval)
	row.LastListedAt = row.EvaluatedAt
	return row
}

func TestSummarizeNoLiveRowsReportsNothing(t *testing.T) {
	t.Parallel()

	for name, rows := range map[string][]Observation{
		"no rows":         nil,
		"only stale rows": {staleRow("sel-a", StateArchivedExcluded), staleRow("sel-b", StateSelected)},
	} {
		if summary, ok := Summarize(rows, summaryNow); ok {
			t.Fatalf("%s: Summarize() = %+v, true; want no summary", name, summary)
		}
	}
}

func TestSummarizeAggregate(t *testing.T) {
	t.Parallel()

	confirmedSince := summaryNow.Add(-summaryInterval)
	tests := []struct {
		name       string
		rows       []Observation
		wantState  Aggregate
		wantReason State
	}{
		{name: "live selected", rows: []Observation{liveRow("sel-a", StateSelected)}, wantState: AggregateSelected},
		{
			name:      "any live selected wins over a live exclusion",
			rows:      []Observation{liveRow("sel-a", StateSelected), liveRow("sel-b", StateRuleExcluded)},
			wantState: AggregateSelected,
		},
		{
			name:       "stale selected row does not rescue a live exclusion",
			rows:       []Observation{staleRow("sel-a", StateSelected), liveRow("sel-b", StateArchivedExcluded)},
			wantState:  AggregateNotSelected,
			wantReason: StateArchivedExcluded,
		},
		{
			name:       "archived exclusion applies immediately",
			rows:       []Observation{liveRow("sel-a", StateArchivedExcluded)},
			wantState:  AggregateNotSelected,
			wantReason: StateArchivedExcluded,
		},
		{
			name:       "rule exclusion applies immediately",
			rows:       []Observation{liveRow("sel-a", StateRuleExcluded)},
			wantState:  AggregateNotSelected,
			wantReason: StateRuleExcluded,
		},
		{
			name:       "one unlisted cycle is pending",
			rows:       []Observation{unlistedRow("sel-a", 1, summaryNow)},
			wantState:  AggregatePending,
			wantReason: StateNotListed,
		},
		{
			name:       "confirmed unlisted is not selected",
			rows:       []Observation{unlistedRow("sel-a", 2, confirmedSince)},
			wantState:  AggregateNotSelected,
			wantReason: StateNotListed,
		},
		{
			name:       "one pending selector keeps the aggregate pending",
			rows:       []Observation{unlistedRow("sel-a", 2, confirmedSince), unlistedRow("sel-b", 1, summaryNow)},
			wantState:  AggregatePending,
			wantReason: StateNotListed,
		},
		{
			name:       "pending unlisted beside an archived exclusion is pending",
			rows:       []Observation{liveRow("sel-a", StateArchivedExcluded), unlistedRow("sel-b", 1, summaryNow)},
			wantState:  AggregatePending,
			wantReason: StateNotListed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			summary, ok := Summarize(tt.rows, summaryNow)
			if !ok {
				t.Fatalf("Summarize() reported no live rows, want %s", tt.wantState)
			}
			if summary.State != tt.wantState || summary.Reason != tt.wantReason {
				t.Fatalf("Summarize() = {%s %s}, want {%s %s}", summary.State, summary.Reason, tt.wantState, tt.wantReason)
			}
		})
	}
}

func TestSummarizeTimestamps(t *testing.T) {
	t.Parallel()

	t.Run("selected reports the newest listing and no unlisted time", func(t *testing.T) {
		t.Parallel()
		older := liveRow("sel-a", StateSelected)
		older.LastListedAt = summaryNow.Add(-2 * time.Minute)
		older.EvaluatedAt = older.LastListedAt
		pending := unlistedRow("sel-b", 1, summaryNow)
		summary, _ := Summarize([]Observation{older, liveRow("sel-c", StateSelected), pending}, summaryNow)
		if !summary.LastListedAt.Equal(summaryNow) || !summary.EvaluatedAt.Equal(summaryNow) {
			t.Fatalf("summary = %+v, want last_listed_at and evaluated_at = %v", summary, summaryNow)
		}
		if !summary.UnlistedSince.IsZero() {
			t.Fatalf("UnlistedSince = %v, want zero for a selected scope", summary.UnlistedSince)
		}
	})

	t.Run("not selected reports the earliest unlisted time across live rows", func(t *testing.T) {
		t.Parallel()
		earliest := summaryNow.Add(-3 * time.Hour)
		a := unlistedRow("sel-a", 9, earliest)
		b := unlistedRow("sel-b", 2, summaryNow.Add(-summaryInterval))
		b.EvaluatedAt = summaryNow.Add(time.Minute)
		summary, _ := Summarize([]Observation{a, b, staleRow("sel-c", StateNotListed)}, summaryNow)
		if !summary.UnlistedSince.Equal(earliest) {
			t.Fatalf("UnlistedSince = %v, want %v", summary.UnlistedSince, earliest)
		}
		if !summary.EvaluatedAt.Equal(b.EvaluatedAt) {
			t.Fatalf("EvaluatedAt = %v, want newest live %v", summary.EvaluatedAt, b.EvaluatedAt)
		}
		if !summary.LastListedAt.Equal(b.LastListedAt) {
			t.Fatalf("LastListedAt = %v, want newest live %v", summary.LastListedAt, b.LastListedAt)
		}
	})

	t.Run("never-listed scope reports a zero last listing", func(t *testing.T) {
		t.Parallel()
		row := unlistedRow("sel-a", 2, summaryNow.Add(-summaryInterval))
		row.LastListedAt = time.Time{}
		summary, _ := Summarize([]Observation{row}, summaryNow)
		if !summary.LastListedAt.IsZero() {
			t.Fatalf("LastListedAt = %v, want zero", summary.LastListedAt)
		}
	})
}

func TestSummarizeReasonComesFromNewestEvaluatedRow(t *testing.T) {
	t.Parallel()

	archived := liveRow("sel-b", StateArchivedExcluded)
	archived.EvaluatedAt = summaryNow.Add(-time.Minute)
	rule := liveRow("sel-a", StateRuleExcluded)
	summary, _ := Summarize([]Observation{archived, rule}, summaryNow)
	if summary.Reason != StateRuleExcluded {
		t.Fatalf("Reason = %s, want rule_excluded from the newest evaluated row", summary.Reason)
	}

	tieA := liveRow("sel-b", StateArchivedExcluded)
	tieB := liveRow("sel-a", StateRuleExcluded)
	first, _ := Summarize([]Observation{tieA, tieB}, summaryNow)
	second, _ := Summarize([]Observation{tieB, tieA}, summaryNow)
	if first.Reason != StateRuleExcluded || second.Reason != StateRuleExcluded {
		t.Fatalf("tie reasons = %s/%s, want rule_excluded from the lowest selector id regardless of order", first.Reason, second.Reason)
	}
}

// TestSummarizeRenamedAndTransferredScopes is the #7625 QA shape: the old
// scope of a renamed repository and a transferred repository stop appearing
// in the complete org listing, while a still-listed scope keeps appearing.
// One cycle later they are pending; two cycles spanning the interval later
// they are not selected; the listed scope stays selected throughout.
func TestSummarizeRenamedAndTransferredScopes(t *testing.T) {
	t.Parallel()

	firstMiss := summaryNow.Add(-summaryInterval)
	afterOneCycle := firstMiss
	afterTwoCycles := summaryNow

	missed := func(evaluatedAt time.Time, cycles int) Observation {
		return Observation{
			SelectorID:         "sel-boatsgroup",
			State:              StateNotListed,
			LastListedAt:       firstMiss.Add(-summaryInterval),
			FirstUnlistedAt:    firstMiss,
			UnlistedCycleCount: cycles,
			EvaluatedAt:        evaluatedAt,
			EvaluationInterval: summaryInterval,
		}
	}
	listed := func(evaluatedAt time.Time) Observation {
		row := liveRow("sel-boatsgroup", StateSelected)
		row.EvaluatedAt, row.LastListedAt = evaluatedAt, evaluatedAt
		return row
	}

	for _, scope := range []string{"renamed old scope r_1f3d8453", "transferred scope"} {
		if got, _ := Summarize([]Observation{missed(afterOneCycle, 1)}, afterOneCycle); got.State != AggregatePending {
			t.Fatalf("%s after one cycle = %s, want pending", scope, got.State)
		}
		if got, _ := Summarize([]Observation{missed(afterTwoCycles, 2)}, afterTwoCycles); got.State != AggregateNotSelected || got.Reason != StateNotListed {
			t.Fatalf("%s after two cycles = {%s %s}, want {not_selected not_listed}", scope, got.State, got.Reason)
		}
	}
	for _, at := range []time.Time{afterOneCycle, afterTwoCycles} {
		if got, _ := Summarize([]Observation{listed(at)}, at); got.State != AggregateSelected {
			t.Fatalf("listed scope at %v = %s, want selected", at, got.State)
		}
	}
}
