// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReadCoordinatorSnapshotHandlesNullableDeactivatedAtAndCreatedAtBacklogFallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 20, 15, 45, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{
						"collector-git-default",
						"git",
						"continuous",
						true,
						true,
						false,
						"",
						now.Add(-15 * time.Second),
						now.Add(-5 * time.Second),
						nil,
					},
				},
			},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{
				rows: [][]any{
					{int64(1), int64(0), 42.0},
				},
			},
		},
	}

	got, err := readCoordinatorSnapshot(context.Background(), queryer, now)
	if err != nil {
		t.Fatalf("readCoordinatorSnapshot() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("readCoordinatorSnapshot() = nil, want snapshot")
	}
	if len(got.CollectorInstances) != 1 {
		t.Fatalf("readCoordinatorSnapshot().CollectorInstances len = %d, want 1", len(got.CollectorInstances))
	}
	if !got.CollectorInstances[0].DeactivatedAt.IsZero() {
		t.Fatalf("readCoordinatorSnapshot().CollectorInstances[0].DeactivatedAt = %v, want zero", got.CollectorInstances[0].DeactivatedAt)
	}
	if got.OldestPendingAge != 42*time.Second {
		t.Fatalf("readCoordinatorSnapshot().OldestPendingAge = %v, want %v", got.OldestPendingAge, 42*time.Second)
	}
	if !strings.Contains(workflowCoordinatorClaimSnapshotQuery, "MIN(COALESCE(visible_at, created_at))") {
		t.Fatalf("workflowCoordinatorClaimSnapshotQuery missing created_at fallback:\n%s", workflowCoordinatorClaimSnapshotQuery)
	}
	if !strings.Contains(workflowCoordinatorClaimSnapshotQuery, "GREATEST(") {
		t.Fatalf("workflowCoordinatorClaimSnapshotQuery must clamp future timestamps to zero age:\n%s", workflowCoordinatorClaimSnapshotQuery)
	}
}

func TestReadCoordinatorSnapshotClampsNegativeOldestPendingAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 21, 14, 15, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{}},
			{rows: [][]any{}},
			{
				rows: [][]any{
					{"pending", int64(1)},
				},
			},
			{rows: [][]any{}},
			{
				rows: [][]any{
					{int64(0), int64(0), -0.256},
				},
			},
		},
	}

	got, err := readCoordinatorSnapshot(context.Background(), queryer, now)
	if err != nil {
		t.Fatalf("readCoordinatorSnapshot() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("readCoordinatorSnapshot() = nil, want snapshot")
	}
	if got.OldestPendingAge < 0 {
		t.Fatalf("readCoordinatorSnapshot().OldestPendingAge = %v, want non-negative", got.OldestPendingAge)
	}
	if got.OldestPendingAge != 0 {
		t.Fatalf("readCoordinatorSnapshot().OldestPendingAge = %v, want 0", got.OldestPendingAge)
	}
}

func TestReadCoordinatorSnapshotIncludesCollectorBackpressure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 4, 20, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{{"failed_retryable", int64(3)}}},
			{rows: [][]any{}},
			{rows: [][]any{{int64(1), int64(0), 0.0}}},
			{rows: [][]any{{int64(0), int64(0), int64(0)}}},
			{rows: [][]any{{
				"package_registry",
				"pkg-registry-primary",
				"package_registry",
				int64(7),
				int64(1),
				int64(3),
				int64(2),
				int64(1),
				int64(1),
				int64(1),
				int64(0),
				300.0,
				90.0,
				120.0,
				45.0,
			}}},
			{rows: [][]any{{"package_registry", "pkg-registry-primary", "package_registry", "provider_rate_limited", int64(3)}}},
		},
	}

	got, err := readCoordinatorSnapshot(context.Background(), queryer, now)
	if err != nil {
		t.Fatalf("readCoordinatorSnapshot() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("readCoordinatorSnapshot() = nil, want snapshot")
	}
	if len(got.CollectorBackpressure) != 1 {
		t.Fatalf("CollectorBackpressure len = %d, want 1: %#v", len(got.CollectorBackpressure), got.CollectorBackpressure)
	}
	row := got.CollectorBackpressure[0]
	if row.Pending != 7 || row.Retrying != 3 || row.DeadLetter != 2 || row.TerminalFailed != 1 {
		t.Fatalf("CollectorBackpressure[0] = %#v, want pending/retrying/dead-letter/terminal counts", row)
	}
	if len(row.FailureClassCounts) != 1 || row.FailureClassCounts[0].Name != "provider_rate_limited" {
		t.Fatalf("FailureClassCounts = %#v, want provider_rate_limited evidence", row.FailureClassCounts)
	}
}

func TestReadWorkflowCoordinatorRecentFailuresCountsWindowedRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 21, 14, 15, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{int64(2), int64(1), int64(3)},
				},
			},
		},
	}

	got, err := readWorkflowCoordinatorRecentFailures(context.Background(), queryer, now, 30*time.Minute)
	if err != nil {
		t.Fatalf("readWorkflowCoordinatorRecentFailures() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("readWorkflowCoordinatorRecentFailures() = nil, want snapshot")
	}
	if got.Window != 30*time.Minute {
		t.Fatalf("recent failures Window = %v, want 30m", got.Window)
	}
	if got.FailedRuns != 2 || got.BlockedCompleteness != 1 || got.TerminalWorkItems != 3 {
		t.Fatalf("recent failures = %#v, want failed=2 blocked=1 terminal=3", got)
	}
	if !got.Active() {
		t.Fatal("recent failures Active() = false, want true")
	}
	// The query must bound by updated_at so aged failures fall out of scope.
	if !strings.Contains(workflowCoordinatorRecentFailuresQuery, "updated_at >= $1") {
		t.Fatalf("workflowCoordinatorRecentFailuresQuery missing updated_at window:\n%s", workflowCoordinatorRecentFailuresQuery)
	}
}

func TestReadWorkflowCoordinatorRecentFailuresDefaultsWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 21, 14, 15, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{{int64(0), int64(0), int64(0)}}},
		},
	}

	got, err := readWorkflowCoordinatorRecentFailures(context.Background(), queryer, now, 0)
	if err != nil {
		t.Fatalf("readWorkflowCoordinatorRecentFailures() error = %v, want nil", err)
	}
	if got.Window != defaultCoordinatorRecentFailureWindow {
		t.Fatalf("recent failures Window = %v, want default %v", got.Window, defaultCoordinatorRecentFailureWindow)
	}
}
