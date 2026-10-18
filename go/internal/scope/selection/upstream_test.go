// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

import (
	"testing"
	"time"
)

var upstreamNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func upstreamObservation(selector string, evaluated time.Time) Observation {
	return Observation{
		SelectorID: selector, State: StateNotListed,
		StateSince: upstreamNow.Add(-time.Hour), StateCycleCount: 3,
		EvaluatedAt: evaluated, LivenessWindow: 48 * time.Hour,
		Classification: UpstreamTransferred, SuccessorSlug: "archive/a",
		ClassifiedAt: upstreamNow.Add(-time.Hour),
	}
}

func TestWithUpstreamAnnotatesNotListed(t *testing.T) {
	t.Parallel()

	observations := []Observation{
		upstreamObservation("sel-b", upstreamNow.Add(-time.Minute)),
		upstreamObservation("sel-a", upstreamNow.Add(-time.Minute)),
	}
	observations[1].Classification = UpstreamRenamed
	observations[1].SuccessorSlug = "acme/a-v2"
	summary, err := Summarize(observations, upstreamNow, func() (time.Time, error) { return time.Time{}, nil })
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	var resolved []string
	annotated := summary.WithUpstream(observations, func(slug string) bool {
		resolved = append(resolved, slug)
		return true
	})
	if annotated.Classification != UpstreamRenamed || annotated.SuccessorSlug != "acme/a-v2" || !annotated.SuccessorSelected {
		t.Fatalf("WithUpstream() = %+v, want the newest decider's renamed verdict with a selected successor", annotated)
	}
	if len(resolved) != 1 || resolved[0] != "acme/a-v2" {
		t.Fatalf("successorSelected calls = %v, want exactly [acme/a-v2]", resolved)
	}
}

func TestWithUpstreamLeavesOtherStatesAlone(t *testing.T) {
	t.Parallel()

	selected := Observation{
		SelectorID: "sel-a", State: StateSelected, LastListedAt: upstreamNow,
		StateSince: upstreamNow.Add(-time.Hour), StateCycleCount: 2,
		EvaluatedAt: upstreamNow, LivenessWindow: 48 * time.Hour,
	}
	summary, err := Summarize([]Observation{selected}, upstreamNow, func() (time.Time, error) { return time.Time{}, nil })
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	called := false
	annotated := summary.WithUpstream([]Observation{selected}, func(string) bool { called = true; return true })
	if annotated.Classification != "" || annotated.SuccessorSlug != "" || annotated.SuccessorSelected {
		t.Fatalf("WithUpstream(selected) = %+v, want no annotation", annotated)
	}
	if called {
		t.Fatal("WithUpstream(selected) resolved a successor: only not_listed annotates")
	}
	unknown, err := Summarize(nil, upstreamNow, func() (time.Time, error) { return time.Time{}, nil })
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if annotated := unknown.WithUpstream(nil, nil); annotated != unknown {
		t.Fatalf("WithUpstream(unknown) = %+v, want %+v", annotated, unknown)
	}
}

func TestSummarizePageEqualsSingleScopeSummarize(t *testing.T) {
	t.Parallel()

	live := map[string][]Observation{
		"scope:transferred": {upstreamObservation("sel-a", upstreamNow)},
		"scope:pending": {{
			SelectorID: "sel-a", State: StateNotListed,
			StateSince: upstreamNow, StateCycleCount: 1,
			EvaluatedAt: upstreamNow, LivenessWindow: 48 * time.Hour,
		}},
		"scope:selected": {{
			SelectorID: "sel-a", State: StateSelected, LastListedAt: upstreamNow,
			StateSince: upstreamNow.Add(-time.Hour), StateCycleCount: 2,
			EvaluatedAt: upstreamNow, LivenessWindow: 48 * time.Hour,
		}},
	}
	latest := func(scopeID string) (time.Time, error) { return time.Time{}, nil }
	successors := func(slug string) bool { return false }
	page, err := SummarizePage(
		[]string{"scope:transferred", "scope:pending", "scope:selected", "scope:unknown"},
		live, upstreamNow, latest, successors,
	)
	if err != nil {
		t.Fatalf("SummarizePage() error = %v", err)
	}
	for scope, observations := range live {
		single, err := Summarize(observations, upstreamNow, func() (time.Time, error) { return latest(scope) })
		if err != nil {
			t.Fatalf("Summarize(%s) error = %v", scope, err)
		}
		single = single.WithUpstream(observations, successors)
		if page[scope] != single {
			t.Fatalf("page[%s] = %+v, want single-scope %+v: the list aggregate must equal the freshness aggregate", scope, page[scope], single)
		}
	}
	// A scope with no live row reads unknown with no annotation.
	if got := page["scope:unknown"]; got.State != AggregateUnknown || got.Classification != "" {
		t.Fatalf("page[unknown] = %+v, want unknown with no annotation", got)
	}
	// The transferred scope carries its verdict and an unselected successor
	// (archive/a is not a selected scope here).
	if got := page["scope:transferred"]; got.State != AggregateNotSelected || got.Reason != StateNotListed ||
		got.Classification != UpstreamTransferred || got.SuccessorSlug != "archive/a" || got.SuccessorSelected {
		t.Fatalf("page[transferred] = %+v, want not_selected/transferred with an unselected successor", got)
	}
}
