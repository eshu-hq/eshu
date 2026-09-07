// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// FakeRepositoryFreshnessReader is the test double for
// RepositoryFreshnessReader. Promoted here for #6060 lane B3: the freshness
// route tests live in package repository while the graph-read-error sweep
// tests stay in package query, and an unexported double in either package
// is unreachable from the other. Fields are exported so both packages can
// build it with keyed literals; GotRepoID captures the repo the handler
// passed for assertion.
type FakeRepositoryFreshnessReader struct {
	Snapshot  status.RepositoryFreshnessSnapshot
	Err       error
	GotRepoID string
}

// ReadRepositoryFreshness implements the freshness-reader port against the
// installed Snapshot, returning Err when set.
func (f *FakeRepositoryFreshnessReader) ReadRepositoryFreshness(_ context.Context, repoID string) (status.RepositoryFreshnessSnapshot, error) {
	f.GotRepoID = repoID
	if f.Err != nil {
		return status.RepositoryFreshnessSnapshot{}, f.Err
	}
	return f.Snapshot, nil
}

// FullyBuiltRepositoryFreshnessSnapshot returns a resolved snapshot with
// generation, commit, and stages populated. Shared by package query and
// package repository freshness tests for the same reason as
// FakeRepositoryFreshnessReader: a builder in either package's _test.go is
// unreachable from the other.
func FullyBuiltRepositoryFreshnessSnapshot() status.RepositoryFreshnessSnapshot {
	activatedAt := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	return status.RepositoryFreshnessSnapshot{
		RepositoryID:  "repo-1",
		ScopeID:       "scope-1",
		Resolved:      true,
		ScopeKind:     "repository",
		HasGeneration: true,
		Generation: status.RepositoryFreshnessGeneration{
			ID: "gen-1", Status: "active", TriggerKind: "push", IsDelta: true, ActivatedAt: activatedAt,
		},
		ObservedCommit: "abc123",
		ObservedAt:     activatedAt.Add(-time.Minute),
		Stages:         status.RepositoryFreshnessStages{Collected: true, Reduced: true, Projected: true, Materialized: true},
	}
}
