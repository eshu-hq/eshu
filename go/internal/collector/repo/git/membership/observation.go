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
// confirmation. It applies selection.Confirmed, the one definition the
// freshness reader also uses, so the gauge and the not_selected verdict
// cannot disagree.
func Confirmed(o Observation) bool {
	return selection.Confirmed(selection.Observation{
		State:              o.State,
		LastListedAt:       o.LastListedAt,
		FirstUnlistedAt:    o.FirstUnlistedAt,
		UnlistedCycleCount: o.UnlistedCycleCount,
		EvaluatedAt:        o.EvaluatedAt,
		EvaluationInterval: o.EvaluationInterval,
	})
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
