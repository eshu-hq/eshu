// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

func terminalFailureInstance(kind, id string, scanInterval time.Duration) collector.InstanceSummary {
	return collector.InstanceSummary{
		InstanceID:    id,
		CollectorKind: kind,
		Mode:          "continuous",
		Enabled:       true,
		ScanInterval:  scanInterval,
	}
}

func TestReadWorkflowCollectorTerminalFailuresScansBoundedColumns(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 23, 12, 30, 0, 0, time.UTC)
	failedAt := asOf.Add(-10 * time.Minute)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		{"security_alert", "security-alert-example", "auth_denied", "list_org_alerts", failedAt},
		{"git", "git-example", "", "", failedAt.Add(-time.Minute)},
	}}}}

	got, err := readWorkflowCollectorTerminalFailures(context.Background(), queryer, []collector.InstanceSummary{
		terminalFailureInstance("git", "git-example", 0),
		terminalFailureInstance("security_alert", "security-alert-example", 0),
	}, asOf)
	if err != nil {
		t.Fatalf("readWorkflowCollectorTerminalFailures() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	first := got[0]
	if first.CollectorKind != "security_alert" || first.InstanceID != "security-alert-example" ||
		first.FailureClass != "auth_denied" || first.FailureOperation != "list_org_alerts" ||
		!first.ObservedAt.Equal(failedAt) {
		t.Fatalf("first = %#v, want security_alert/auth_denied/list_org_alerts/terminal", first)
	}
}

// TestReadWorkflowCollectorTerminalFailuresBindsPerInstanceWindow proves the
// reader computes one cutoff per eligible instance from its scan_interval
// (3 x scan_interval, 30 minute floor) against the snapshot asOf, and never
// asks about disabled or deactivated instances (#8803).
func TestReadWorkflowCollectorTerminalFailuresBindsPerInstanceWindow(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 23, 12, 30, 0, 0, time.UTC)
	disabled := terminalFailureInstance("git", "git-disabled", 0)
	disabled.Enabled = false
	deactivated := terminalFailureInstance("git", "git-deactivated", 0)
	deactivated.DeactivatedAt = asOf.Add(-time.Hour)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}}}

	if _, err := readWorkflowCollectorTerminalFailures(context.Background(), queryer, []collector.InstanceSummary{
		terminalFailureInstance("git", "git-default", 0),
		disabled,
		deactivated,
		terminalFailureInstance("security_alert", "sa-15m", 15*time.Minute),
		terminalFailureInstance("security_alert", "sa-12h", 12*time.Hour),
	}, asOf); err != nil {
		t.Fatalf("readWorkflowCollectorTerminalFailures() error = %v, want nil", err)
	}
	if len(queryer.args) != 1 || len(queryer.args[0]) != 3 {
		t.Fatalf("args = %#v, want one call with three array arguments", queryer.args)
	}
	kinds, _ := queryer.args[0][0].([]string)
	ids, _ := queryer.args[0][1].([]string)
	cutoffs, _ := queryer.args[0][2].([]time.Time)
	wantIDs := []string{"git-default", "sa-15m", "sa-12h"}
	if strings.Join(ids, ",") != strings.Join(wantIDs, ",") || strings.Join(kinds, ",") != "git,security_alert,security_alert" {
		t.Fatalf("kinds = %v ids = %v, want only the enabled, non-deactivated instances %v", kinds, ids, wantIDs)
	}
	wantCutoffs := []time.Time{
		asOf.Add(-30 * time.Minute), // default 30s interval: the 30 minute floor
		asOf.Add(-45 * time.Minute), // 3 x 15m
		asOf.Add(-36 * time.Hour),   // 3 x 12h
	}
	if len(cutoffs) != len(wantCutoffs) {
		t.Fatalf("cutoffs = %v, want %v", cutoffs, wantCutoffs)
	}
	for i := range wantCutoffs {
		if !cutoffs[i].Equal(wantCutoffs[i]) {
			t.Fatalf("cutoff[%d] = %v, want %v", i, cutoffs[i], wantCutoffs[i])
		}
	}
}

func TestReadWorkflowCollectorTerminalFailuresSkipsReadWithoutEligibleInstances(t *testing.T) {
	t.Parallel()

	disabled := terminalFailureInstance("git", "git-disabled", 0)
	disabled.Enabled = false
	queryer := &fakeQueryer{}
	got, err := readWorkflowCollectorTerminalFailures(context.Background(), queryer, []collector.InstanceSummary{disabled}, time.Now())
	if err != nil || got != nil {
		t.Fatalf("readWorkflowCollectorTerminalFailures() = (%v, %v), want (nil, nil)", got, err)
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("issued %d queries, want none without an eligible instance", len(queryer.queries))
	}
}

func TestTerminalFailureCutoffWindow(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		interval time.Duration
		want     time.Duration
	}{
		{0, 30 * time.Minute},
		{30 * time.Second, 30 * time.Minute},
		{10 * time.Minute, 30 * time.Minute},
		{15 * time.Minute, 45 * time.Minute},
		{12 * time.Hour, 36 * time.Hour},
		{-time.Hour, 30 * time.Minute},
	} {
		if got := asOf.Sub(terminalFailureCutoff(asOf, tc.interval)); got != tc.want {
			t.Fatalf("window for scan_interval %s = %s, want %s", tc.interval, got, tc.want)
		}
	}
	// An absurd interval must not overflow into a future cutoff.
	if cutoff := terminalFailureCutoff(asOf, time.Duration(1<<62)); !cutoff.Before(asOf) {
		t.Fatalf("cutoff for a huge interval = %v, want before %v", cutoff, asOf)
	}
}

func TestListCoordinatorCollectorInstancesReadsScanInterval(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	row := func(id, scanInterval any) []any {
		return []any{id, "security_alert", "continuous", true, false, true, "", now, now, nil, scanInterval}
	}
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		row("sa-set", `"12h"`),
		row("sa-unset", nil),
		row("sa-malformed", `3600`),
		row("sa-bad-duration", `"twelve hours"`),
	}}}}
	got, err := listCoordinatorCollectorInstances(context.Background(), queryer)
	if err != nil {
		t.Fatalf("listCoordinatorCollectorInstances() error = %v, want nil", err)
	}
	want := map[string]time.Duration{"sa-set": 12 * time.Hour, "sa-unset": 0, "sa-malformed": 0, "sa-bad-duration": 0}
	for _, instance := range got {
		if instance.ScanInterval != want[instance.InstanceID] {
			t.Fatalf("%s ScanInterval = %s, want %s", instance.InstanceID, instance.ScanInterval, want[instance.InstanceID])
		}
	}
	if !strings.Contains(workflowCoordinatorCollectorInstancesQuery, "configuration->'scan_interval'") {
		t.Fatalf("instances query does not select the scan_interval value:\n%s", workflowCoordinatorCollectorInstancesQuery)
	}
}

func TestWorkflowCollectorTerminalFailuresQueryShape(t *testing.T) {
	t.Parallel()

	query := workflowCollectorTerminalFailuresQuery
	for _, forbidden := range []string{"last_failure_message", "last_failure_status_code", "failure_message", "last_failure_terminal", "LIMIT 20"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("query contains %q:\n%s", forbidden, query)
		}
	}
	normalized := strings.Join(strings.Fields(query), " ")
	for _, want := range []string{
		"FROM unnest($1::text[], $2::text[], $3::timestamptz[]) AS i(collector_kind, instance_id, cutoff) CROSS JOIN LATERAL",
		"t.status = 'failed_terminal'",
		"t.updated_at >= i.cutoff",
		"NOT EXISTS",
		"s.collector_instance_id = t.collector_instance_id",
		"s.scope_id = t.scope_id",
		"s.status = 'completed'",
		"s.updated_at > t.updated_at",
		"ORDER BY t.updated_at DESC, t.work_item_id ASC LIMIT 1",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}

func TestReadCoordinatorSnapshotIncludesTerminalFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 30, 0, 0, time.UTC)
	failedAt := now.Add(-10 * time.Minute)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{{"security-alert-example", "security_alert", "continuous", true, false, true, "", now, now, nil, nil}}},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{{int64(1), int64(0), 0.0}}},
			{rows: [][]any{{int64(0), int64(0), int64(0)}}},
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{{"security_alert", "security-alert-example", "auth_denied", "list_org_alerts", failedAt}}},
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
	// One asOf drives the window: the failure read's cutoff is derived from
	// the same snapshot time the rest of the snapshot uses.
	last := queryer.args[len(queryer.args)-1]
	cutoffs, _ := last[2].([]time.Time)
	if len(cutoffs) != 1 || !cutoffs[0].Equal(now.Add(-30*time.Minute)) {
		t.Fatalf("cutoffs = %v, want [%v]", cutoffs, now.Add(-30*time.Minute))
	}
}
