// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestRenderStatusIncludesCollectorBackpressure(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 6, 17, 4, 10, 0, 0, time.UTC),
			Coordinator: &status.CoordinatorSnapshot{
				CollectorBackpressure: []status.CollectorBackpressureSnapshot{{
					CollectorKind:       "package_registry",
					CollectorInstanceID: "pkg-registry-primary",
					SourceSystem:        "package_registry",
					Pending:             7,
					Claimed:             1,
					Retrying:            3,
					DeadLetter:          2,
					TerminalFailed:      1,
					Expired:             1,
					ActiveClaims:        1,
					OverdueClaims:       1,
					OldestPendingAge:    5 * time.Minute,
					OldestRetryAge:      90 * time.Second,
					OldestClaimAge:      2 * time.Minute,
					NextRetryDelay:      45 * time.Second,
					FailureClassCounts: []status.NamedCount{
						{Name: "provider_rate_limited", Count: 3},
						{Name: "provider_unavailable", Count: 1},
					},
				}},
			},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		"\"collector_backpressure\"",
		"\"collector_kind\": \"package_registry\"",
		"\"collector_instance_id\": \"pkg-registry-primary\"",
		"\"source_system\": \"package_registry\"",
		"\"pending\": 7",
		"\"retrying\": 3",
		"\"dead_letter\": 2",
		"\"overdue_claims\": 1",
		"\"oldest_pending_age_seconds\": 300",
		"\"oldest_retry_age_seconds\": 90",
		"\"oldest_claim_age_seconds\": 120",
		"\"next_retry_delay_seconds\": 45",
		"\"name\": \"provider_rate_limited\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("RenderJSON() missing %q in payload:\n%s", want, body)
		}
	}

	text := status.RenderText(report)
	for _, want := range []string{
		"Collector backpressure:",
		"package_registry/pkg-registry-primary source_system=package_registry pending=7 claimed=1 retrying=3 dead_letter=2 terminal=1 expired=1",
		"active_claims=1 overdue_claims=1 oldest_pending=5m0s oldest_retry=1m30s oldest_claim=2m0s next_retry=45s",
		"failure_classes=provider_rate_limited=3,provider_unavailable=1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("RenderText() missing %q in output:\n%s", want, text)
		}
	}
}
