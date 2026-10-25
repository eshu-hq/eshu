// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// hostedActivationConfiguration is a complete component activation document:
// the stored configuration of an extension-hosted instance (#8951).
const hostedActivationConfiguration = `{"schema_version":"eshu.component.instance.v1","component_id":"vendor.example",` +
	`"component_version":"1.0.0","manifest_digest":"sha256:abc","config_handle":"h",` +
	`"runtime":{"sdk_protocol":"collector.sdk/v1alpha2","adapter":"oci"}}`

// hostedFixtureClasses is the raw persisted classes the surface tests feed:
// one the extension host pattern admits (a vendor class), one common class,
// and three the bounding must keep unknown whatever the instance is.
var hostedFixtureClasses = map[string]string{
	"vendor_quota_exceeded": collector.ExtensionDefinedFailureToken,
	"collect_failure":       "collect_failure",
	"Free Text Message":     collector.UnknownFailureToken,
	"":                      collector.UnknownFailureToken,
	strings.Repeat("a", 97): collector.UnknownFailureToken,
}

// stubRows replays fixed rows through db.Rows with the scan targets the
// terminal-failure reads use, including the nullable acknowledged_at pointer
// the shared status fake cannot scan.
type stubRows struct {
	rows [][]any
	i    int
}

func (r *stubRows) Next() bool { return r.i < len(r.rows) }
func (r *stubRows) Err() error { return nil }
func (r *stubRows) Close() error {
	return nil
}

func (r *stubRows) Scan(dest ...any) error {
	row := r.rows[r.i]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = row[i].(string)
		case *bool:
			*target = row[i].(bool)
		case *time.Time:
			*target = row[i].(time.Time)
		case **time.Time:
			if value, ok := row[i].(time.Time); ok {
				*target = &value
			}
		case *sql.NullTime:
			if value, ok := row[i].(time.Time); ok {
				*target = sql.NullTime{Time: value, Valid: true}
			}
		case *sql.NullString:
			if value, ok := row[i].(string); ok {
				*target = sql.NullString{String: value, Valid: true}
			}
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	r.i++
	return nil
}

// routedQueryer answers each store read by its SQL text, so a test controls
// the instance registry and the failure rows independently and records the
// order of reads.
type routedQueryer struct {
	instances [][]any
	failures  [][]any
	reads     []string
}

func (q *routedQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("routedQueryer does not execute writes")
}

func (q *routedQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	switch query {
	case workflowCoordinatorCollectorInstancesQuery:
		q.reads = append(q.reads, "instances")
		return &stubRows{rows: q.instances}, nil
	case terminalFailureListQuery:
		q.reads = append(q.reads, "list")
		return &stubRows{rows: q.failures}, nil
	case terminalFailureAckTargetQuery:
		q.reads = append(q.reads, "target")
		return &stubRows{rows: q.failures}, nil
	case terminalFailureAckListQuery:
		q.reads = append(q.reads, "acks")
		return &stubRows{rows: q.failures}, nil
	}
	return nil, fmt.Errorf("unexpected query %q", query)
}

func failureTime() time.Time { return time.Date(2026, 10, 24, 11, 30, 0, 0, time.UTC) }

// TestListTerminalFailuresBoundsHostedFirstPartyAsExtensionDefined pins #8951
// on the admin failed-scope list: a component-activated instance of a
// first-party kind reads extension_defined for a class its extension defines,
// the in-tree instance of the same kind keeps unknown, and free text, an empty
// class, and an over-long class stay unknown for both.
func TestListTerminalFailuresBoundsHostedFirstPartyAsExtensionDefined(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		configuration string
		hosted        bool
	}{
		"hosted":  {hostedActivationConfiguration, true},
		"in-tree": {`{}`, false},
	} {
		for raw, want := range hostedFixtureClasses {
			t.Run(fmt.Sprintf("%s/%q", name, raw), func(t *testing.T) {
				t.Parallel()
				expected := want
				if want == collector.ExtensionDefinedFailureToken && !tc.hosted {
					expected = collector.UnknownFailureToken
				}
				queryer := &routedQueryer{
					instances: [][]any{instanceRowWithConfiguration("jira-1", "jira", true, nil, tc.configuration)},
					failures:  [][]any{{"scope-1", raw, "fetch", failureTime(), "work-1", false, nil}},
				}
				store := NewTerminalFailureAckStore(queryer, nil)
				page, err := store.ListTerminalFailures(context.Background(), TerminalFailureListFilter{
					CollectorKind: "jira", InstanceID: "jira-1",
				})
				if err != nil || len(page.Items) != 1 {
					t.Fatalf("page = %+v, %v; want one item", page, err)
				}
				if got := page.Items[0].FailureClass; got != expected {
					t.Fatalf("FailureClass(%q) on %s = %q, want %q", raw, name, got, expected)
				}
			})
		}
	}
}

// TestResolveTargetBoundsHostedFirstPartyAsExtensionDefined pins #8951 on the
// acknowledgement target (the class the acknowledge response and audit echo).
// Acknowledge resolves its target through the same resolveTarget, so this
// covers the write response as well.
func TestResolveTargetBoundsHostedFirstPartyAsExtensionDefined(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		configuration string
		want          string
	}{
		"hosted":  {hostedActivationConfiguration, collector.ExtensionDefinedFailureToken},
		"in-tree": {`{}`, collector.UnknownFailureToken},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			queryer := &routedQueryer{
				instances: [][]any{instanceRowWithConfiguration("jira-1", "jira", true, nil, tc.configuration)},
				failures:  [][]any{{"work-1", "vendor_quota_exceeded", "fetch", failureTime()}},
			}
			store := NewTerminalFailureAckStore(queryer, nil)
			target, err := store.ResolveTarget(context.Background(), TerminalFailureAckRequest{
				CollectorKind: "jira", InstanceID: "jira-1", ScopeID: "scope-1",
			})
			if err != nil {
				t.Fatalf("ResolveTarget() error = %v", err)
			}
			if target.FailureClass != tc.want {
				t.Fatalf("target FailureClass on %s = %q, want %q", name, target.FailureClass, tc.want)
			}
			if target.rawClass != "vendor_quota_exceeded" {
				t.Fatalf("rawClass = %q; the stored row must keep the raw class the covering rule matches", target.rawClass)
			}
		})
	}
}

// TestListTerminalFailureAcksBoundsEachRecordByItsOwnInstance pins #8951 on the
// stored acknowledgement records: each record reads the class of its own
// instance, so a hosted and an in-tree jira instance in one listing differ, a
// record whose instance no longer exists keeps the kind-only reading, and the
// instance registry is read once, after the record rows are closed.
func TestListTerminalFailureAcksBoundsEachRecordByItsOwnInstance(t *testing.T) {
	t.Parallel()
	ackedAt := failureTime().Add(time.Minute)
	record := func(instance string) []any {
		return []any{
			"jira", instance, "scope-1", "vendor_quota_exceeded", failureTime(), "work-1",
			"reason", "operator", "corr-1", ackedAt,
		}
	}
	queryer := &routedQueryer{
		instances: [][]any{
			instanceRowWithConfiguration("hosted", "jira", true, nil, hostedActivationConfiguration),
			instanceRowWithConfiguration("in-tree", "jira", true, nil, `{}`),
		},
		failures: [][]any{record("hosted"), record("in-tree"), record("removed")},
	}
	store := NewTerminalFailureAckStore(queryer, nil)
	got, err := store.ListTerminalFailureAcks(context.Background(), TerminalFailureAckListFilter{})
	if err != nil || len(got) != 3 {
		t.Fatalf("records = %+v, %v; want three", got, err)
	}
	want := map[string]string{
		"hosted":  collector.ExtensionDefinedFailureToken,
		"in-tree": collector.UnknownFailureToken,
		"removed": collector.UnknownFailureToken,
	}
	for _, r := range got {
		if r.FailureClass != want[r.InstanceID] {
			t.Fatalf("record %s FailureClass = %q, want %q", r.InstanceID, r.FailureClass, want[r.InstanceID])
		}
	}
	if strings.Join(queryer.reads, ",") != "acks,instances" {
		t.Fatalf("reads = %v, want the ack rows then one instance registry read", queryer.reads)
	}
}

// TestListTerminalFailureAcksSkipsTheInstanceReadWhenEmpty pins that an empty
// listing costs no registry read.
func TestListTerminalFailureAcksSkipsTheInstanceReadWhenEmpty(t *testing.T) {
	t.Parallel()
	queryer := &routedQueryer{}
	store := NewTerminalFailureAckStore(queryer, nil)
	got, err := store.ListTerminalFailureAcks(context.Background(), TerminalFailureAckListFilter{})
	if err != nil || len(got) != 0 {
		t.Fatalf("records = %+v, %v; want none", got, err)
	}
	if strings.Join(queryer.reads, ",") != "acks" {
		t.Fatalf("reads = %v, want only the ack read", queryer.reads)
	}
}
