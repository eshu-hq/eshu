// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selection

import "time"

// State is the stored selection state of one repository scope under one
// selector. The values match the repository_selection_observations state
// CHECK constraint (migration 164).
type State string

const (
	// StateSelected means the selector selects the repository: a complete
	// org listing names it and a rule matches, or an explicit collector is
	// configured with it.
	StateSelected State = "selected"
	// StateArchivedExcluded means the listing names the repository as
	// archived and the selector does not include archived repositories.
	StateArchivedExcluded State = "archived_excluded"
	// StateRuleExcluded means the listing names the repository and no
	// configured rule matches it.
	StateRuleExcluded State = "rule_excluded"
	// StateNotListed means a complete listing did not name the repository.
	StateNotListed State = "not_listed"
)

// ConfirmationMinSpan is the shortest time a row must hold an exclusion state
// across at least two evaluations before Confirmed accepts it. It guards
// against two evaluations in quick succession (a restart, a replica) both
// seeing the same transient listing gap.
const ConfirmationMinSpan = 5 * time.Minute

// Observation is the selection evidence one selector stored for one scope.
// Zero times stand for SQL NULL.
type Observation struct {
	SelectorID string
	State      State
	// LastListedAt is the newest evaluation whose listing named the scope.
	LastListedAt time.Time
	// StateSince is the evaluation at which the row entered State.
	StateSince time.Time
	// StateCycleCount is how many evaluations in a row stored State,
	// counting the one at StateSince as 1.
	StateCycleCount int
	EvaluatedAt     time.Time
	// LivenessWindow is how long after EvaluatedAt the row still counts; the
	// writing collector stores its ESHU_REPO_SELECTION_LIVENESS_WINDOW.
	LivenessWindow time.Duration
}

// Live reports whether o still counts at now: evaluated_at + liveness window
// >= now. A row past its window belongs to a selector that stopped evaluating
// (removed, reconfigured, or down) and decides nothing. A row without a
// positive window is never live. The repository freshness read filters rows
// with the same predicate in SQL; a live Postgres test keeps the two equal.
func Live(o Observation, now time.Time) bool {
	if o.LivenessWindow <= 0 {
		return false
	}
	return !o.EvaluatedAt.Add(o.LivenessWindow).Before(now)
}

// Confirmed reports whether o is settled exclusion evidence: any state other
// than selected, held for at least two evaluations spanning at least
// ConfirmationMinSpan. The rule is the same for not_listed, archived_excluded,
// and rule_excluded, so a first evaluation never confirms anything.
func Confirmed(o Observation) bool {
	return o.State != StateSelected &&
		o.StateCycleCount >= 2 &&
		o.EvaluatedAt.Sub(o.StateSince) >= ConfirmationMinSpan
}
