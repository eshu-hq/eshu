// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestStatusReportToMapIncludesBuildVersion(t *testing.T) {
	t.Parallel()

	report := status.Report{
		AsOf:   time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC),
		Health: status.HealthSummary{State: "healthy"},
		Queue:  status.QueueSnapshot{},
	}

	payload := statusReportToMap(report)
	if got, want := payload["version"], buildinfo.AppVersion(); got != want {
		t.Fatalf("statusReportToMap(report)[version] = %#v, want %#v", got, want)
	}
}

func TestStatusReportToMapIncludesCollectorGenerationDeadLetters(t *testing.T) {
	t.Parallel()

	report := status.Report{
		AsOf:   time.Date(2026, 6, 12, 19, 0, 0, 0, time.UTC),
		Health: status.HealthSummary{State: "degraded"},
		CollectorGenerationDeadLetters: status.CollectorGenerationDeadLetterSnapshot{
			DeadLetter:          2,
			ReplayRequested:     1,
			ReplayAttempts:      3,
			OldestDeadLetterAge: 2 * time.Minute,
		},
	}

	payload := statusReportToMap(report)
	raw, ok := payload["collector_generation_dead_letters"].(map[string]any)
	if !ok {
		t.Fatalf("collector_generation_dead_letters = %#v, want map", payload["collector_generation_dead_letters"])
	}
	if got, want := raw["dead_letter"], 2; got != want {
		t.Fatalf("dead_letter = %#v, want %#v", got, want)
	}
	if got, want := raw["replay_requested"], 1; got != want {
		t.Fatalf("replay_requested = %#v, want %#v", got, want)
	}
	if got, want := raw["oldest_dead_letter_age_ms"], int64(120000); got != want {
		t.Fatalf("oldest_dead_letter_age_ms = %#v, want %#v", got, want)
	}
}

// TestStatusReportToMapIncludesDomainBacklogTruncation proves the
// /api/v0/status/pipeline map carries the same truncation metadata as
// RenderText/RenderJSON (#4045 review: statusReportToMapWithRaw has its own
// serializer, and BuildReport recording DomainBacklogsTruncated does not by
// itself reach a caller reading /api/v0/status/pipeline instead of
// /admin/status).
func TestStatusReportToMapIncludesDomainBacklogTruncation(t *testing.T) {
	t.Parallel()

	report := status.Report{
		AsOf:                    time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		Health:                  status.HealthSummary{State: "healthy"},
		DomainBacklogsTruncated: true,
		DomainBacklogsLimit:     5,
	}

	payload := statusReportToMap(report)
	if got, want := payload["domain_backlogs_truncated"], true; got != want {
		t.Fatalf("domain_backlogs_truncated = %#v, want %#v", got, want)
	}
	if got, want := payload["domain_backlogs_limit"], 5; got != want {
		t.Fatalf("domain_backlogs_limit = %#v, want %#v", got, want)
	}
}

// TestStatusReportToMapZeroesDomainBacklogLimitWhenNotTruncated is the
// negative control: a caller must not read an untruncated response as capped
// at whatever DomainLimit happened to be configured.
func TestStatusReportToMapZeroesDomainBacklogLimitWhenNotTruncated(t *testing.T) {
	t.Parallel()

	report := status.Report{
		AsOf:                    time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		Health:                  status.HealthSummary{State: "healthy"},
		DomainBacklogsTruncated: false,
		DomainBacklogsLimit:     5,
	}

	payload := statusReportToMap(report)
	if got, want := payload["domain_backlogs_truncated"], false; got != want {
		t.Fatalf("domain_backlogs_truncated = %#v, want %#v", got, want)
	}
	if got, want := payload["domain_backlogs_limit"], 0; got != want {
		t.Fatalf("domain_backlogs_limit = %#v, want %#v (guarded to zero when not truncated)", got, want)
	}
}
