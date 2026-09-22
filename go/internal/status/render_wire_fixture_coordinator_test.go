// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// fixtureCoordinatorSnapshot returns a fully populated *CoordinatorSnapshot,
// including CollectorInstances, CollectorBackpressure, and RecentFailures,
// for maxRawSnapshot (render_wire_fixture_test.go).
func fixtureCoordinatorSnapshot() *CoordinatorSnapshot {
	return &CoordinatorSnapshot{
		CollectorInstances: []collector.InstanceSummary{
			{
				InstanceID:     "collector-git-1",
				CollectorKind:  "git",
				Mode:           "direct",
				Enabled:        true,
				Bootstrap:      true,
				ClaimsEnabled:  false,
				DisplayName:    "Git Fixture Instance",
				LastObservedAt: fixtureTime(100),
				UpdatedAt:      fixtureTime(101),
				DeactivatedAt:  fixtureTime(102),
			},
			{
				InstanceID:     "collector-terraform-1",
				CollectorKind:  "terraform_state",
				Mode:           "claim_driven",
				Enabled:        true,
				Bootstrap:      false,
				ClaimsEnabled:  true,
				DisplayName:    "Terraform State Fixture Instance",
				LastObservedAt: fixtureTime(103),
				UpdatedAt:      fixtureTime(104),
				DeactivatedAt:  fixtureTime(105),
			},
		},
		RunStatusCounts: []NamedCount{
			{Name: "collection_active", Count: 5},
			{Name: "failed", Count: 2},
		},
		WorkItemStatusCounts: []NamedCount{
			{Name: "pending", Count: 7},
			{Name: "failed_terminal", Count: 1},
		},
		CompletenessCounts: []NamedCount{
			{Name: "pending", Count: 3},
			{Name: "blocked", Count: 1},
		},
		CollectorBackpressure: []collector.BackpressureSnapshot{
			{
				CollectorKind:       "git",
				CollectorInstanceID: "collector-git-1",
				SourceSystem:        "github",
				Pending:             9,
				Claimed:             4,
				Retrying:            2,
				DeadLetter:          1,
				TerminalFailed:      1,
				Expired:             1,
				ActiveClaims:        3,
				OverdueClaims:       1,
				OldestPendingAge:    fixtureDuration(910),
				OldestRetryAge:      fixtureDuration(920),
				OldestClaimAge:      fixtureDuration(930),
				NextRetryDelay:      fixtureDuration(940),
				FailureClassCounts: []NamedCount{
					{Name: "transient_backend_error", Count: 3},
					{Name: "rate_limited", Count: 1},
				},
			},
			{
				CollectorKind:       "terraform_state",
				CollectorInstanceID: "collector-terraform-1",
				SourceSystem:        "s3",
				Pending:             6,
				Claimed:             2,
				Retrying:            1,
				DeadLetter:          1,
				TerminalFailed:      1,
				Expired:             0,
				ActiveClaims:        1,
				OverdueClaims:       0,
				OldestPendingAge:    fixtureDuration(950),
				OldestRetryAge:      fixtureDuration(960),
				OldestClaimAge:      fixtureDuration(970),
				NextRetryDelay:      fixtureDuration(980),
				FailureClassCounts: []NamedCount{
					{Name: "credential_error", Count: 2},
					{Name: "timeout", Count: 1},
				},
			},
		},
		ActiveClaims:     4,
		OverdueClaims:    2,
		OldestPendingAge: fixtureDuration(990),
		RecentFailures: &CoordinatorRecentFailures{
			Window:              fixtureDuration(3600),
			FailedRuns:          3,
			BlockedCompleteness: 1,
			TerminalWorkItems:   2,
		},
	}
}

// fixtureRegistryCollectors returns []RegistryCollectorSnapshot with every
// exported field populated, including FailureClassCounts and
// MetadataTargetCounts, for maxRawSnapshot (render_wire_fixture_test.go).
func fixtureRegistryCollectors() []RegistryCollectorSnapshot {
	return []RegistryCollectorSnapshot{
		{
			CollectorKind:              "oci_registry",
			ConfiguredInstances:        3,
			ActiveScopes:               12,
			RecentCompletedGenerations: 8,
			LastCompletedAt:            fixtureTime(110),
			RetryableFailures:          2,
			TerminalFailures:           1,
			FailureClassCounts: []NamedCount{
				{Name: "rate_limited", Count: 2},
				{Name: "timeout", Count: 1},
			},
			MetadataTargetCounts: []RegistryMetadataTargetCount{
				{Ecosystem: "oci", Planned: 20, Completed: 15, Skipped: 2, Stale: 1, Failed: 1, RateLimited: 1},
				{Ecosystem: "helm", Planned: 10, Completed: 8, Skipped: 1, Stale: 0, Failed: 1, RateLimited: 0},
			},
		},
		{
			CollectorKind:              "package_registry",
			ConfiguredInstances:        2,
			ActiveScopes:               6,
			RecentCompletedGenerations: 4,
			LastCompletedAt:            fixtureTime(111),
			RetryableFailures:          1,
			TerminalFailures:           0,
			FailureClassCounts: []NamedCount{
				{Name: "not_found", Count: 4},
				{Name: "timeout", Count: 2},
			},
			MetadataTargetCounts: []RegistryMetadataTargetCount{
				{Ecosystem: "npm", Planned: 30, Completed: 25, Skipped: 3, Stale: 1, Failed: 1, RateLimited: 0},
				{Ecosystem: "go", Planned: 15, Completed: 12, Skipped: 1, Stale: 1, Failed: 1, RateLimited: 0},
			},
		},
	}
}
