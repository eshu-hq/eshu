// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"context"
	"time"
)

// Repository freshness retirement verdicts (#7766). They are rule 0 of
// ComputeRepositoryFreshnessVerdict, ahead of every commit, build,
// selection, and unknown rule: the tombstone is the operator's explicit
// retirement act, so a fenced repository never answers current or building
// for evidence the fence already froze.
const (
	// RepositoryFreshnessRetiring marks a repository with an open
	// non-complete retirement tombstone: fenced, still serving its
	// phase-1-frozen graph until the runner retracts it.
	RepositoryFreshnessRetiring RepositoryFreshnessVerdict = "retiring"
	// RepositoryFreshnessRetired marks a repository whose retirement
	// tombstone is complete: purged, served as tombstone history.
	RepositoryFreshnessRetired RepositoryFreshnessVerdict = "retired"
)

// RepositoryFreshnessRetirement is the bounded tombstone evidence a
// freshness snapshot carries for rule 0 (#7766). It names the tombstone
// state and row only: counts and lifecycle timestamps stay on the admin
// readback, never on the per-repository surface.
type RepositoryFreshnessRetirement struct {
	// State is the tombstone state (pending, running, blocked,
	// repairing_graph, complete, failed).
	State string
	// RetirementID is the tombstone key.
	RetirementID string
}

// IsComplete reports whether the tombstone is retirement history rather
// than an in-flight retirement.
func (r *RepositoryFreshnessRetirement) IsComplete() bool {
	return r != nil && r.State == "complete"
}

// RetirementSummary is the bounded tombstone aggregate behind the
// index-status repository_retirements block (#7766). Counts only, no ids:
// per-state open-row counts, the oldest pending age derived from
// requested_at, and the last completion from retired_at.
type RetirementSummary struct {
	// CountsByState counts open tombstone rows by state.
	CountsByState map[string]int64
	// HasPending reports at least one open pending row, so the age below
	// is meaningful rather than a zero over nothing.
	HasPending bool
	// OldestPendingAgeSeconds is now minus the oldest open pending row's
	// requested_at. Zero when HasPending is false.
	OldestPendingAgeSeconds float64
	// HasCompleted reports at least one complete row, so the timestamp
	// below is meaningful rather than a zero time.
	HasCompleted bool
	// LastCompletedAt is the newest complete row's retired_at. Zero when
	// HasCompleted is false.
	LastCompletedAt time.Time
}

// IndexRetirementReader reads the tombstone aggregate for the index-status
// surface. Production wiring passes the Postgres retirement store; nil
// disables the block and the surface omits it.
type IndexRetirementReader interface {
	ReadRetirementSummary(ctx context.Context) (RetirementSummary, error)
}
