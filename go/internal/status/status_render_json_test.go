// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestBuildReportClampsNegativeAges(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 5, 21, 14, 15, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding:          1,
				Pending:              1,
				OldestOutstandingAge: -256 * time.Millisecond,
			},
			DomainBacklogs: []status.DomainBacklog{{
				Domain:      "source_local",
				Outstanding: 1,
				OldestAge:   -500 * time.Millisecond,
			}},
			Coordinator: &status.CoordinatorSnapshot{
				RunStatusCounts: []status.NamedCount{
					{Name: "collection_active", Count: 1},
				},
				OldestPendingAge: -1 * time.Second,
			},
		},
		status.DefaultOptions(),
	)

	if report.Queue.OldestOutstandingAge < 0 {
		t.Fatalf("BuildReport().Queue.OldestOutstandingAge = %v, want non-negative", report.Queue.OldestOutstandingAge)
	}
	if got, want := report.Queue.OldestOutstandingAge, time.Duration(0); got != want {
		t.Fatalf("BuildReport().Queue.OldestOutstandingAge = %v, want %v", got, want)
	}
	if got, want := report.DomainBacklogs[0].OldestAge, time.Duration(0); got != want {
		t.Fatalf("BuildReport().DomainBacklogs[0].OldestAge = %v, want %v", got, want)
	}
	if report.Coordinator == nil {
		t.Fatal("BuildReport().Coordinator = nil, want coordinator snapshot")
	}
	if got, want := report.Coordinator.OldestPendingAge, time.Duration(0); got != want {
		t.Fatalf("BuildReport().Coordinator.OldestPendingAge = %v, want %v", got, want)
	}
}

func TestRenderJSONNormalizesCoordinatorSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 20, 15, 45, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances: []status.CollectorInstanceSummary{{
					InstanceID:     "collector-git-default",
					CollectorKind:  "git",
					Mode:           "continuous",
					Enabled:        true,
					Bootstrap:      true,
					ClaimsEnabled:  false,
					LastObservedAt: now,
					UpdatedAt:      now,
				}},
				RunStatusCounts:      []status.NamedCount{{Name: "collection_pending", Count: 1}},
				WorkItemStatusCounts: []status.NamedCount{{Name: "claimed", Count: 1}},
				ActiveClaims:         1,
			},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}

	body := string(payload)
	if !strings.Contains(body, "\"collector_instances\"") {
		t.Fatalf("RenderJSON() = %s, want coordinator collector instances", payload)
	}
	if !strings.Contains(body, "\"name\": \"collection_pending\"") {
		t.Fatalf("RenderJSON() = %s, want lower-case named count keys", payload)
	}
	if strings.Contains(body, "\"Name\"") {
		t.Fatalf("RenderJSON() = %s, want no exported-case named count keys", payload)
	}
	if strings.Contains(body, "0001-01-01T00:00:00Z") {
		t.Fatalf("RenderJSON() = %s, want zero deactivated_at omitted", payload)
	}
}

func TestRenderStatusIncludesRegistryCollectors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 14, 30, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			RegistryCollectors: []status.RegistryCollectorSnapshot{{
				CollectorKind:              "oci_registry",
				ConfiguredInstances:        1,
				ActiveScopes:               2,
				RecentCompletedGenerations: 4,
				LastCompletedAt:            now.Add(-2 * time.Minute),
				RetryableFailures:          1,
				FailureClassCounts: []status.NamedCount{{
					Name:  "registry_rate_limited",
					Count: 1,
				}},
				MetadataTargetCounts: []status.RegistryMetadataTargetCount{{
					Ecosystem:   "npm",
					Planned:     5,
					Completed:   3,
					Skipped:     1,
					Stale:       1,
					Failed:      1,
					RateLimited: 1,
				}},
			}},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		"\"registry_collectors\"",
		"\"collector_kind\": \"oci_registry\"",
		"\"active_scopes\": 2",
		"\"recent_completed_generations\": 4",
		"\"name\": \"registry_rate_limited\"",
		"\"metadata_targets\"",
		"\"ecosystem\": \"npm\"",
		"\"planned\": 5",
		"\"rate_limited\": 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("RenderJSON() = %s, want %q", payload, want)
		}
	}
	text := status.RenderText(report)
	if !strings.Contains(text, "Registry collectors:") ||
		!strings.Contains(text, "failure_classes=registry_rate_limited=1") ||
		!strings.Contains(text, "metadata_targets=npm(planned=5 completed=3 skipped=1 stale=1 failed=1 rate_limited=1)") {
		t.Fatalf("RenderText() = %s, want registry collector summary", text)
	}
}

func TestRenderStatusIncludesAWSCloudScans(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 14, 30, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			AWSCloudScans: []status.AWSCloudScanStatus{{
				CollectorInstanceID: "aws-prod",
				AccountID:           "123456789012",
				Region:              "us-east-1",
				ServiceKind:         "ecr",
				Status:              "partial",
				CommitStatus:        "committed",
				FailureClass:        "budget_exhausted",
				APICallCount:        51,
				ThrottleCount:       3,
				WarningCount:        1,
				BudgetExhausted:     true,
				LastCompletedAt:     now.Add(-2 * time.Minute),
			}},
			AWSCloudScansTruncated: true,
			AWSCloudScanLimit:      1000,
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		"\"aws_cloud_scans\"",
		"\"collector_instance_id\": \"aws-prod\"",
		"\"status\": \"partial\"",
		"\"commit_status\": \"committed\"",
		"\"throttle_count\": 3",
		"\"budget_exhausted\": true",
		"\"aws_cloud_scans_truncated\": true",
		"\"aws_cloud_scan_limit\": 1000",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("RenderJSON() = %s, want %q", payload, want)
		}
	}

	text := status.RenderText(report)
	if !strings.Contains(text, "AWS cloud scans:") ||
		!strings.Contains(text, "123456789012/us-east-1/ecr status=partial commit=committed") ||
		!strings.Contains(text, "api_calls=51 throttles=3 warnings=1") ||
		!strings.Contains(text, "failure=budget_exhausted") ||
		!strings.Contains(text, "AWS cloud scans truncated: limit=1000") {
		t.Fatalf("RenderText() = %s, want AWS cloud scan summary", text)
	}
}
