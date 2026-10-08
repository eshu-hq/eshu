// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

import (
	"errors"
	"fmt"
	"time"
)

// Aggregate is the scope-level selection outcome across every live selector.
// The set is closed; the freshness response renders it as selection.state.
type Aggregate string

const (
	// AggregateSelected means at least one live selector selects the scope.
	AggregateSelected Aggregate = "selected"
	// AggregateNotSelected means every live selector has confirmed exclusion
	// evidence and no generation of the scope was observed after the newest
	// exclusion started: rules (a) through (d) all hold.
	AggregateNotSelected Aggregate = "not_selected"
	// AggregatePendingConfirmation means no live selector selects the scope
	// but at least one live row has not passed Confirmed yet.
	AggregatePendingConfirmation Aggregate = "pending_confirmation"
	// AggregateExcludedStillIngested means rules (a) through (c) hold but a
	// generation of the scope was observed after the newest exclusion
	// started, so something still ingests it (rule (d) fails).
	AggregateExcludedStillIngested Aggregate = "excluded_still_ingested"
	// AggregateUnknown means the scope has no live row, so selection
	// evidence says nothing.
	AggregateUnknown Aggregate = "unknown"
)

// Summary is the scope-level selection evidence computed from the scope's
// live observations. Zero times mean the value is absent; for
// AggregateUnknown every field but State is zero.
type Summary struct {
	State Aggregate
	// Reason is empty for AggregateSelected and AggregateUnknown. Otherwise
	// it is the state of the most recently evaluated live row, with the
	// lowest selector id breaking a tie.
	Reason State
	// StateSince is the earliest state_since among live selected rows when
	// selected (the longest-running live selection), otherwise the latest
	// state_since across live rows: when the last live selector stopped
	// selecting, which rule (d) compares against.
	StateSince time.Time
	// LastListedAt is the newest last_listed_at among the deciding rows: the
	// live selected rows when selected, otherwise every live row.
	LastListedAt time.Time
	// EvaluatedAt is the newest evaluated_at among the deciding rows.
	EvaluatedAt time.Time
	// LiveSelectorCount is how many live rows the scope has, one per
	// selector.
	LiveSelectorCount int
}

// Summarize aggregates a scope's observations as of now. Let L be the rows
// that are Live. The scope is not_selected iff:
//
//	(a) L is not empty,
//	(b) no row in L is selected,
//	(c) every row in L is Confirmed, and
//	(d) G <= max(state_since over L),
//
// where G is the newest observed_at over every generation of the scope.
// latestGeneration supplies G and is called at most once, and only after (a)
// through (c) hold, so a selected or pending scope never pays for the read.
// It returns the zero time when the scope has no generation, which satisfies
// (d). When (a) through (c) hold but (d) fails the state is
// excluded_still_ingested. An error from latestGeneration, or a nil
// latestGeneration when it is needed, is returned with a zero Summary.
func Summarize(observations []Observation, now time.Time, latestGeneration func() (time.Time, error)) (Summary, error) {
	var (
		live        []Observation
		anySelected bool
		allSettled  = true
	)
	for _, o := range observations {
		if !Live(o, now) {
			continue
		}
		live = append(live, o)
		if o.State == StateSelected {
			anySelected = true
		}
		if !Confirmed(o) {
			allSettled = false
		}
	}
	if len(live) == 0 {
		return Summary{State: AggregateUnknown}, nil
	}

	summary := Summary{LiveSelectorCount: len(live)}
	var (
		newest     Observation
		haveNewest bool
	)
	for _, o := range live {
		if anySelected && o.State != StateSelected {
			continue
		}
		if o.LastListedAt.After(summary.LastListedAt) {
			summary.LastListedAt = o.LastListedAt
		}
		if o.EvaluatedAt.After(summary.EvaluatedAt) {
			summary.EvaluatedAt = o.EvaluatedAt
		}
		switch {
		case summary.StateSince.IsZero():
			summary.StateSince = o.StateSince
		case anySelected && o.StateSince.Before(summary.StateSince):
			summary.StateSince = o.StateSince
		case !anySelected && o.StateSince.After(summary.StateSince):
			summary.StateSince = o.StateSince
		}
		if !haveNewest || newerDecider(o, newest) {
			newest, haveNewest = o, true
		}
	}
	if anySelected {
		summary.State = AggregateSelected
		return summary, nil
	}
	summary.Reason = newest.State
	if !allSettled {
		summary.State = AggregatePendingConfirmation
		return summary, nil
	}
	if latestGeneration == nil {
		return Summary{}, errors.New("summarize repository selection: latest generation reader is required")
	}
	latest, err := latestGeneration()
	if err != nil {
		return Summary{}, fmt.Errorf("summarize repository selection: %w", err)
	}
	summary.State = AggregateNotSelected
	if latest.After(summary.StateSince) {
		summary.State = AggregateExcludedStillIngested
	}
	return summary, nil
}

// newerDecider orders candidate rows for the reason: the newest evaluated_at
// wins and the lowest selector id breaks ties, so the reason does not depend
// on row order.
func newerDecider(candidate, current Observation) bool {
	if !candidate.EvaluatedAt.Equal(current.EvaluatedAt) {
		return candidate.EvaluatedAt.After(current.EvaluatedAt)
	}
	return candidate.SelectorID < current.SelectorID
}
