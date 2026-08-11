// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// domainBacklogsFixture returns n distinct, non-empty domain backlog rows so
// tests can exercise BuildReport's top-N cap (status.go topDomainBacklogs)
// without every row colliding on Domain or Outstanding.
func domainBacklogsFixture(n int) []status.DomainBacklog {
	rows := make([]status.DomainBacklog, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, status.DomainBacklog{
			Domain:      string(rune('a' + i)),
			Outstanding: n - i,
		})
	}
	return rows
}

// TestBuildReportFlagsDomainBacklogTruncation proves BuildReport records that
// DomainBacklogs was capped by opts.DomainLimit (#4045 review: a capped
// backlog list must be distinguishable from a complete one, since a
// downstream composer such as the live evidence bundle route can only avoid
// presenting a partial domain list as complete if BuildReport tells it
// truncation happened).
func TestBuildReportFlagsDomainBacklogTruncation(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf:           time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		DomainBacklogs: domainBacklogsFixture(7),
	}, status.Options{DomainLimit: 5})

	if len(report.DomainBacklogs) != 5 {
		t.Fatalf("len(DomainBacklogs) = %d, want 5 (capped)", len(report.DomainBacklogs))
	}
	if !report.DomainBacklogsTruncated {
		t.Fatal("DomainBacklogsTruncated = false, want true when 7 domains exceed a limit of 5")
	}
	if report.DomainBacklogsLimit != 5 {
		t.Fatalf("DomainBacklogsLimit = %d, want 5", report.DomainBacklogsLimit)
	}
}

// TestBuildReportDoesNotFlagTruncationWhenWithinLimit is the negative
// control: a domain count at or under the limit must not read as truncated.
func TestBuildReportDoesNotFlagTruncationWhenWithinLimit(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf:           time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		DomainBacklogs: domainBacklogsFixture(5),
	}, status.Options{DomainLimit: 5})

	if len(report.DomainBacklogs) != 5 {
		t.Fatalf("len(DomainBacklogs) = %d, want 5", len(report.DomainBacklogs))
	}
	if report.DomainBacklogsTruncated {
		t.Fatal("DomainBacklogsTruncated = true, want false when domain count equals the limit")
	}
}
