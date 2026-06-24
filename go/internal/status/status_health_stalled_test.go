// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestBuildReportStalledBacklogNamesLargestDomain(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding:          94,
				Pending:              94,
				OldestOutstandingAge: 15 * time.Minute,
			},
			DomainBacklogs: []status.DomainBacklog{
				{
					Domain:      "source_local",
					Outstanding: 94,
					OldestAge:   15 * time.Minute,
				},
			},
		},
		status.Options{
			StallAfter:  10 * time.Minute,
			DomainLimit: 5,
		},
	)

	if report.Health.State != "stalled" {
		t.Fatalf("BuildReport().Health.State = %q, want stalled", report.Health.State)
	}
	if len(report.Health.Reasons) == 0 ||
		!strings.Contains(report.Health.Reasons[0], "source_local") ||
		!strings.Contains(report.Health.Reasons[0], "94 outstanding") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want source_local backlog detail", report.Health.Reasons)
	}
}

func TestBuildReportStalledBacklogFallsBackToQueueAge(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
			Queue: status.QueueSnapshot{
				Outstanding:          4,
				Pending:              4,
				OldestOutstandingAge: 15 * time.Minute,
			},
			DomainBacklogs: []status.DomainBacklog{
				{
					Domain:      "source_local",
					Outstanding: 4,
				},
			},
		},
		status.Options{
			StallAfter:  10 * time.Minute,
			DomainLimit: 5,
		},
	)

	if len(report.Health.Reasons) == 0 ||
		!strings.Contains(report.Health.Reasons[0], "for 15m0s") {
		t.Fatalf("BuildReport().Health.Reasons = %v, want queue age fallback", report.Health.Reasons)
	}
}
