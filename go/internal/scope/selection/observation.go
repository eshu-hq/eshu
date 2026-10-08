// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

import "time"

// State is the stored selection state of one repository scope under one
// selector. The values match the repository_selection_observations state
// CHECK constraint (migration 163).
type State string

const (
	// StateSelected means the complete listing names the repository and the
	// selector's rules select it.
	StateSelected State = "selected"
	// StateArchivedExcluded means the listing names the repository as
	// archived and the selector does not include archived repositories.
	StateArchivedExcluded State = "archived_excluded"
	// StateRuleExcluded means the listing names the repository and no
	// configured rule matches it.
	StateRuleExcluded State = "rule_excluded"
	// StateNotListed means a complete listing did not name the repository.
	// It is evidence only after two-cycle confirmation; see Confirmed.
	StateNotListed State = "not_listed"
)

// LiveIntervals is how many of its own evaluation intervals an observation
// stays live after evaluated_at. A row older than that belongs to a selector
// that stopped evaluating (removed, reconfigured, or down) and must not
// decide anything.
const LiveIntervals = 3

// Observation is the selection evidence one selector stored for one scope.
// Zero times stand for SQL NULL.
type Observation struct {
	SelectorID         string
	State              State
	LastListedAt       time.Time
	FirstUnlistedAt    time.Time
	UnlistedCycleCount int
	EvaluatedAt        time.Time
	EvaluationInterval time.Duration
}

// Live reports whether o was evaluated within LiveIntervals of its own
// evaluation interval before now. An observation without a positive interval
// is never live.
func Live(o Observation, now time.Time) bool {
	if o.EvaluationInterval <= 0 {
		return false
	}
	return !o.EvaluatedAt.Before(now.Add(-LiveIntervals * o.EvaluationInterval))
}

// Confirmed reports whether a not_listed observation passed two-cycle
// confirmation: at least two consecutive unlisted cycles spanning at least
// the stored evaluation interval. Positive listing evidence (archived or
// rule excluded) needs no confirmation and is never "confirmed" here.
func Confirmed(o Observation) bool {
	return o.State == StateNotListed &&
		o.UnlistedCycleCount >= 2 &&
		o.EvaluatedAt.Sub(o.FirstUnlistedAt) >= o.EvaluationInterval
}

// Excluded reports whether o is settled evidence that its selector no longer
// selects the scope: an archived or rule exclusion, which applies
// immediately, or a confirmed not_listed observation.
func Excluded(o Observation) bool {
	switch o.State {
	case StateArchivedExcluded, StateRuleExcluded:
		return true
	case StateNotListed:
		return Confirmed(o)
	default:
		return false
	}
}
