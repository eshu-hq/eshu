// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReadWorkflowCollectorTerminalFailuresScansBoundedColumns(t *testing.T) {
	t.Parallel()

	failedAt := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		{"security_alert", "security-alert-example", "auth_denied", "list_org_alerts", true, failedAt},
		{"git", "git-example", "", "", true, failedAt.Add(-time.Hour)},
	}}}}

	got, err := readWorkflowCollectorTerminalFailures(context.Background(), queryer)
	if err != nil {
		t.Fatalf("readWorkflowCollectorTerminalFailures() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	first := got[0]
	if first.CollectorKind != "security_alert" || first.InstanceID != "security-alert-example" ||
		first.FailureClass != "auth_denied" || first.FailureOperation != "list_org_alerts" ||
		!first.Terminal || !first.ObservedAt.Equal(failedAt) {
		t.Fatalf("first = %#v, want security_alert/auth_denied/list_org_alerts/terminal", first)
	}
}

func TestWorkflowCollectorTerminalFailuresQueryShape(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{"last_failure_message", "last_failure_status_code", "failure_message"} {
		if strings.Contains(workflowCollectorTerminalFailuresQuery, forbidden) {
			t.Fatalf("query selects %q, which can carry provider text:\n%s", forbidden, workflowCollectorTerminalFailuresQuery)
		}
	}
	for _, want := range []string{
		"status = 'failed_terminal'",
		"NOT EXISTS",
		"s.status = 'completed'",
		"s.updated_at > c.updated_at",
		"LIMIT 20",
		"i.enabled",
		"i.deactivated_at IS NULL",
	} {
		if !strings.Contains(workflowCollectorTerminalFailuresQuery, want) {
			t.Fatalf("query missing %q:\n%s", want, workflowCollectorTerminalFailuresQuery)
		}
	}
}

func TestReadCoordinatorSnapshotIncludesTerminalFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 30, 0, 0, time.UTC)
	failedAt := now.Add(-10 * time.Minute)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{{int64(1), int64(0), 0.0}}},
			{rows: [][]any{{int64(0), int64(0), int64(0)}}},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{{"security_alert", "security-alert-example", "auth_denied", "list_org_alerts", true, failedAt}}},
		},
	}

	got, err := readCoordinatorSnapshot(context.Background(), queryer, now)
	if err != nil {
		t.Fatalf("readCoordinatorSnapshot() error = %v, want nil", err)
	}
	if got == nil || len(got.CollectorTerminalFailures) != 1 {
		t.Fatalf("CollectorTerminalFailures = %#v, want one row", got)
	}
	if got.CollectorTerminalFailures[0].FailureClass != "auth_denied" {
		t.Fatalf("row = %#v, want auth_denied", got.CollectorTerminalFailures[0])
	}
}
