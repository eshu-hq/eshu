// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

// State is the persisted selection state of one repository scope for one
// selector. It aliases selection.State so the writer and the freshness reader
// share one state set.
type State = selection.State

// The persisted states, re-exported from the selection package.
const (
	StateSelected         = selection.StateSelected
	StateArchivedExcluded = selection.StateArchivedExcluded
	StateRuleExcluded     = selection.StateRuleExcluded
	StateNotListed        = selection.StateNotListed
)

// Observation is the stored selection evidence for one scope under one
// selector. Zero times and a zero GitHubRepoID stand for SQL NULL.
// StateSince is when the row entered State and StateCycleCount is how many
// evaluations in a row have written State; LivenessWindow is the window the
// writing collector was configured with.
type Observation struct {
	ScopeID         string
	State           State
	GitHubRepoID    int64
	LastListedAt    time.Time
	StateSince      time.Time
	StateCycleCount int
	EvaluatedAt     time.Time
	LivenessWindow  time.Duration
}

// Confirmed reports whether an excluded observation passed confirmation. It
// applies selection.Confirmed, the one definition the freshness reader also
// uses, so the gauge and the not_selected verdict cannot disagree.
func Confirmed(o Observation) bool {
	return selection.Confirmed(selection.Observation{
		State:           o.State,
		LastListedAt:    o.LastListedAt,
		StateSince:      o.StateSince,
		StateCycleCount: o.StateCycleCount,
		EvaluatedAt:     o.EvaluatedAt,
		LivenessWindow:  o.LivenessWindow,
	})
}

// Row is one scope's input to the batched observation upsert. The store
// derives last_listed_at, state_since, and state_cycle_count from the stored
// row and the batch time.
type Row struct {
	ScopeID      string
	State        State
	GitHubRepoID int64
}

// Batch is one evaluation's upsert: every row shares the selector, the
// evaluation time, and the liveness window. A stored row only advances when
// EvaluatedAt is later than its own evaluated_at.
type Batch struct {
	Selector       Selector
	EvaluatedAt    time.Time
	LivenessWindow time.Duration
	Rows           []Row
}

// project returns the observation the store holds after applying row at now
// over prior. It mirrors the upsert SQL exactly, so the gauge reports the
// post-write state without a read back: an advance-only no-op when prior is
// not older; otherwise state_since and state_cycle_count carry over (plus one
// cycle) when the state repeats and restart at now and 1 when it changes or
// the row is new; last_listed_at moves to now on any listing.
func project(prior Observation, hasPrior bool, row Row, now time.Time, window time.Duration) Observation {
	if hasPrior && !prior.EvaluatedAt.Before(now) {
		return prior
	}
	next := Observation{
		ScopeID:         row.ScopeID,
		State:           row.State,
		GitHubRepoID:    row.GitHubRepoID,
		StateSince:      now,
		StateCycleCount: 1,
		EvaluatedAt:     now,
		LivenessWindow:  window,
	}
	if hasPrior {
		next.LastListedAt = prior.LastListedAt
		if next.GitHubRepoID == 0 {
			next.GitHubRepoID = prior.GitHubRepoID
		}
		if prior.State == row.State {
			next.StateSince = prior.StateSince
			next.StateCycleCount = prior.StateCycleCount + 1
		}
	}
	if row.State != StateNotListed {
		next.LastListedAt = now
	}
	return next
}
