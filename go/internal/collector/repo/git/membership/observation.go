// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import "time"

// State is the persisted selection state of one repository scope for one
// selector. The values match the repository_selection_observations state
// CHECK constraint.
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

// Observation is the stored selection evidence for one scope under one
// selector. Zero times and a zero GitHubRepoID stand for SQL NULL.
type Observation struct {
	ScopeID            string
	State              State
	GitHubRepoID       int64
	LastListedAt       time.Time
	FirstUnlistedAt    time.Time
	UnlistedCycleCount int
	EvaluatedAt        time.Time
	EvaluationInterval time.Duration
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

// Row is one scope's input to the batched observation upsert. The store
// derives last_listed_at, first_unlisted_at, and the unlisted cycle counter
// from the stored row and the batch time.
type Row struct {
	ScopeID      string
	State        State
	GitHubRepoID int64
}

// Batch is one evaluation's upsert: every row shares the selector, the
// evaluation time, and the evaluation interval. A stored row only advances
// when EvaluatedAt is later than its own evaluated_at.
type Batch struct {
	Selector    Selector
	EvaluatedAt time.Time
	Interval    time.Duration
	Rows        []Row
}

// project returns the observation the store holds after applying row at now
// over prior. It mirrors the upsert SQL so the gauge reports the post-write
// state without a read back: an advance-only no-op when prior is not older,
// counters advanced on a repeated miss, and reset on any listing.
func project(prior Observation, hasPrior bool, row Row, now time.Time, interval time.Duration) Observation {
	if hasPrior && !prior.EvaluatedAt.Before(now) {
		return prior
	}
	next := Observation{
		ScopeID:            row.ScopeID,
		State:              row.State,
		GitHubRepoID:       row.GitHubRepoID,
		EvaluatedAt:        now,
		EvaluationInterval: interval,
	}
	if hasPrior {
		next.LastListedAt = prior.LastListedAt
		if next.GitHubRepoID == 0 {
			next.GitHubRepoID = prior.GitHubRepoID
		}
	}
	if row.State != StateNotListed {
		next.LastListedAt = now
		return next
	}
	next.UnlistedCycleCount = 1
	next.FirstUnlistedAt = now
	if hasPrior {
		next.UnlistedCycleCount = prior.UnlistedCycleCount + 1
		if !prior.FirstUnlistedAt.IsZero() {
			next.FirstUnlistedAt = prior.FirstUnlistedAt
		}
	}
	return next
}
