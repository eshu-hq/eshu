// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// listQueryer wraps fakeQueryer with the Exec half the store's database field
// requires; the list never writes.
type listQueryer struct{ *fakeQueryer }

func (listQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

// instanceRow is one collector_instances row in the shape
// workflowCoordinatorCollectorInstancesQuery returns: instance id, kind, mode,
// enabled, bootstrap, claims enabled, display name, last observed, updated,
// deactivated, scan_interval, configuration. The configuration is the in-tree
// empty document, so the instance is not extension-hosted.
func instanceRow(id, kind string, enabled bool, deactivatedAt any) []any {
	return instanceRowWithConfiguration(id, kind, enabled, deactivatedAt, `{}`)
}

// instanceRowWithConfiguration is instanceRow with an explicit stored
// configuration text, which decides whether the instance is extension-hosted.
func instanceRowWithConfiguration(id, kind string, enabled bool, deactivatedAt any, configuration string) []any {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	return []any{id, kind, "continuous", enabled, false, true, "", now, now, deactivatedAt, nil, configuration}
}

// TestListTerminalFailuresBindsTheClampedPageSize pins the limit contract
// without a database: the page size the SQL receives is limit+1 (one extra scope
// to detect truncation) after the default of 100 and the cap of 500, so a request
// for more than the cap, and the cap constant itself, cannot drift unseen.
func TestListTerminalFailuresBindsTheClampedPageSize(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		limit int
		want  int
	}{
		"default when unset":    {0, defaultTerminalFailureListLimit + 1},
		"default when negative": {-5, defaultTerminalFailureListLimit + 1},
		"passed through":        {50, 51},
		"exactly the cap":       {500, 501},
		"clamped to the cap":    {100000, 501},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			queryer := &fakeQueryer{responses: []fakeRows{
				{rows: [][]any{instanceRow("alerts", "security_alert", true, nil)}},
				{},
			}}
			store := NewTerminalFailureAckStore(listQueryer{queryer}, nil)
			if _, err := store.ListTerminalFailures(context.Background(), TerminalFailureListFilter{
				CollectorKind: "security_alert", InstanceID: "alerts", Limit: tc.limit,
			}); err != nil {
				t.Fatalf("ListTerminalFailures() error = %v", err)
			}
			if len(queryer.args) != 2 {
				t.Fatalf("queries = %d, want the instance read and the list read", len(queryer.args))
			}
			args := queryer.args[1]
			if len(args) != 5 || args[4] != tc.want {
				t.Fatalf("list args = %#v, want a page size of %d as the fifth argument", args, tc.want)
			}
		})
	}
	if maxTerminalFailureListLimit != 500 || defaultTerminalFailureListLimit != 100 {
		t.Fatalf("limits = %d default, %d max; the API contract is 100 and 500", defaultTerminalFailureListLimit, maxTerminalFailureListLimit)
	}
}

// TestListTerminalFailuresReadsNothingForAnInactiveInstance pins that a
// disabled, deactivated, kind-mismatched, or unknown instance lists nothing and
// never reaches the failure query.
func TestListTerminalFailuresReadsNothingForAnInactiveInstance(t *testing.T) {
	t.Parallel()
	deactivated := time.Date(2026, 10, 24, 11, 0, 0, 0, time.UTC)
	for name, row := range map[string][]any{
		"deactivated":   instanceRow("alerts", "security_alert", true, deactivated),
		"disabled":      instanceRow("alerts", "security_alert", false, nil),
		"kind mismatch": instanceRow("alerts", "git", true, nil),
		"other id":      instanceRow("someone-else", "security_alert", true, nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{row}}}}
			store := NewTerminalFailureAckStore(listQueryer{queryer}, nil)
			page, err := store.ListTerminalFailures(context.Background(), TerminalFailureListFilter{
				CollectorKind: "security_alert", InstanceID: "alerts",
			})
			if err != nil || len(page.Items) != 0 || page.Truncated {
				t.Fatalf("page = %+v, %v; want an empty page", page, err)
			}
			if len(queryer.args) != 1 {
				t.Fatalf("queries = %d, want only the instance read (no failure query for an inactive instance)", len(queryer.args))
			}
		})
	}
}
