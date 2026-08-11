// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"strings"
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

// TestRenderTextAndJSONIncludeDomainBacklogTruncation proves both operator
// surfaces expose DomainBacklogsTruncated/DomainBacklogsLimit when
// BuildReport capped the domain list (#4045 review: RenderText and
// RenderJSON have separate rendering paths, and the AGENTS.md invariant
// requires a new Report field to appear in both; the AWS cloud scan
// truncation fields are the precedent this mirrors).
func TestRenderTextAndJSONIncludeDomainBacklogTruncation(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf:           time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		DomainBacklogs: domainBacklogsFixture(7),
	}, status.Options{DomainLimit: 5})

	text := status.RenderText(report)
	if !strings.Contains(text, "Domain backlogs truncated: limit=5") {
		t.Fatalf("RenderText() = %s, want domain backlog truncation line", text)
	}

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		"\"domain_backlogs_truncated\": true",
		"\"domain_backlogs_limit\": 5",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("RenderJSON() = %s, want %q", payload, want)
		}
	}
}

// TestRenderTextAndJSONOmitDomainBacklogTruncationWhenNotTruncated is the
// negative control: neither surface should mention truncation when
// BuildReport did not cap the domain list.
func TestRenderTextAndJSONOmitDomainBacklogTruncationWhenNotTruncated(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf:           time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		DomainBacklogs: domainBacklogsFixture(5),
	}, status.Options{DomainLimit: 5})

	text := status.RenderText(report)
	if strings.Contains(text, "Domain backlogs truncated") {
		t.Fatalf("RenderText() = %s, want no domain backlog truncation line", text)
	}

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	body := string(payload)
	if strings.Contains(body, "domain_backlogs_truncated") || strings.Contains(body, "domain_backlogs_limit") {
		t.Fatalf("RenderJSON() = %s, want no domain backlog truncation fields", payload)
	}
}
