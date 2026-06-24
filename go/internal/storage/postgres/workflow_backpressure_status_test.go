// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReadWorkflowCollectorBackpressureStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 17, 4, 10, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{{
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
					int64(1),
					300.0,
					90.0,
					120.0,
					45.0,
				}},
			},
			{
				rows: [][]any{
					{"package_registry", "pkg-registry-primary", "package_registry", "provider_rate_limited", int64(3)},
					{"package_registry", "pkg-registry-primary", "package_registry", "provider_unavailable", int64(1)},
				},
			},
		},
	}

	got, err := readWorkflowCollectorBackpressureStatus(context.Background(), queryer, now)
	if err != nil {
		t.Fatalf("readWorkflowCollectorBackpressureStatus() error = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("backpressure rows = %d, want 1: %#v", len(got), got)
	}
	row := got[0]
	if row.CollectorKind != "package_registry" || row.CollectorInstanceID != "pkg-registry-primary" || row.SourceSystem != "package_registry" {
		t.Fatalf("backpressure identity = %#v, want bounded collector identity", row)
	}
	if row.Pending != 7 || row.Claimed != 1 || row.Retrying != 3 || row.DeadLetter != 2 || row.TerminalFailed != 1 || row.Expired != 1 {
		t.Fatalf("backpressure counts = %#v, want pending/claimed/retrying/dead-letter/terminal/expired", row)
	}
	if row.ActiveClaims != 1 || row.OverdueClaims != 1 {
		t.Fatalf("claim counts = active %d overdue %d, want 1/1", row.ActiveClaims, row.OverdueClaims)
	}
	if row.OldestPendingAge != 5*time.Minute || row.OldestRetryAge != 90*time.Second ||
		row.OldestClaimAge != 2*time.Minute || row.NextRetryDelay != 45*time.Second {
		t.Fatalf("durations = %#v, want pending=5m retry=90s claim=2m next=45s", row)
	}
	if len(row.FailureClassCounts) != 2 || row.FailureClassCounts[0].Name != "provider_rate_limited" || row.FailureClassCounts[0].Count != 3 {
		t.Fatalf("failure class counts = %#v, want provider_rate_limited first", row.FailureClassCounts)
	}

	joined := strings.Join(queryer.queries, "\n")
	for _, want := range []string{
		"workflow_collector_backpressure",
		"GROUP BY collector_kind, collector_instance_id, source_system",
		"FROM collector_generation_dead_letters",
		"last_failure_class",
		"status = 'pending' AND visible_at > $1",
		"status = 'pending' AND visible_at IS NOT NULL",
		"visible_at > $1",
		"lease_expires_at < $1",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("backpressure query missing %q:\n%s", want, joined)
		}
	}
	for _, forbidden := range []string{
		"scope_id",
		"acceptance_unit_id",
		"source_run_id",
		"generation_id",
		"last_failure_message",
	} {
		if strings.Contains(workflowCollectorBackpressureQuery, forbidden) {
			t.Fatalf("backpressure query must not expose high-cardinality %q:\n%s", forbidden, workflowCollectorBackpressureQuery)
		}
	}
	for _, forbidden := range []string{
		"status IN ('pending', 'claimed', 'failed_retryable', 'failed_terminal', 'expired', 'dead_letter')",
		"WHERE status IN ('failed_retryable', 'failed_terminal', 'expired', 'dead_letter')",
	} {
		if strings.Contains(workflowCollectorBackpressureQuery, forbidden) ||
			strings.Contains(workflowCollectorBackpressureFailureClassQuery, forbidden) {
			t.Fatalf("workflow backpressure must not query nonexistent workflow dead_letter status %q", forbidden)
		}
	}
}
