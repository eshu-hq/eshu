// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

import "time"

// Aggregate is the scope-level selection outcome across every live selector.
type Aggregate string

const (
	// AggregateSelected means at least one live selector selects the scope.
	AggregateSelected Aggregate = "selected"
	// AggregateNotSelected means no live selector selects the scope and every
	// live observation is settled exclusion evidence; see Excluded.
	AggregateNotSelected Aggregate = "not_selected"
	// AggregatePending means no live selector selects the scope but at least
	// one live not_listed observation has not passed two-cycle confirmation.
	AggregatePending Aggregate = "pending"
)

// Summary is the scope-level selection evidence computed from the scope's
// live observations. Zero times mean the value is absent.
type Summary struct {
	State Aggregate
	// Reason is empty for AggregateSelected, StateNotListed for
	// AggregatePending, and for AggregateNotSelected the state of the most
	// recently evaluated live row (ties go to the lowest selector id).
	Reason State
	// LastListedAt is the newest last_listed_at among the deciding rows: the
	// live selected rows when selected, otherwise every live row.
	LastListedAt time.Time
	// UnlistedSince is the earliest first_unlisted_at among live not_listed
	// rows. It is zero when the scope is selected.
	UnlistedSince time.Time
	// EvaluatedAt is the newest evaluated_at among the deciding rows.
	EvaluatedAt time.Time
}

// Summarize aggregates a scope's observations as of now. It ignores rows that
// are not Live and reports false when no live row remains, which means the
// selection evidence must not change anything a caller would otherwise
// decide.
func Summarize(observations []Observation, now time.Time) (Summary, bool) {
	var (
		live        []Observation
		anySelected bool
		allExcluded = true
	)
	for _, o := range observations {
		if !Live(o, now) {
			continue
		}
		live = append(live, o)
		if o.State == StateSelected {
			anySelected = true
		}
		if !Excluded(o) {
			allExcluded = false
		}
	}
	if len(live) == 0 {
		return Summary{}, false
	}

	var summary Summary
	switch {
	case anySelected:
		summary.State = AggregateSelected
	case allExcluded:
		summary.State = AggregateNotSelected
	default:
		summary.State = AggregatePending
		summary.Reason = StateNotListed
	}

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
		if o.State == StateNotListed && !o.FirstUnlistedAt.IsZero() &&
			(summary.UnlistedSince.IsZero() || o.FirstUnlistedAt.Before(summary.UnlistedSince)) {
			summary.UnlistedSince = o.FirstUnlistedAt
		}
		if !haveNewest || newerDecider(o, newest) {
			newest, haveNewest = o, true
		}
	}
	if summary.State == AggregateNotSelected {
		summary.Reason = newest.State
	}
	return summary, true
}

// newerDecider orders candidate rows for the not_selected reason: the newest
// evaluated_at wins and the lowest selector id breaks ties, so the reason does
// not depend on row order.
func newerDecider(candidate, current Observation) bool {
	if !candidate.EvaluatedAt.Equal(current.EvaluatedAt) {
		return candidate.EvaluatedAt.After(current.EvaluatedAt)
	}
	return candidate.SelectorID < current.SelectorID
}
