// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestFakeGovernanceAuditAppenderRecordsEveryEvent pins the rule the audit
// tests actually assert on: one Append call carrying several events records all
// of them, in order. Recording only the first would let a handler that emits a
// deny event alongside its allow event pass a test asserting on a single one.
func TestFakeGovernanceAuditAppenderRecordsEveryEvent(t *testing.T) {
	t.Parallel()

	var appender querytestutil.FakeGovernanceAuditAppender

	err := appender.Append(context.Background(), []governanceaudit.Event{
		{ReasonCode: "first"},
		{ReasonCode: "second"},
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if len(appender.Events) != 2 {
		t.Fatalf("Events = %#v, want two recorded events", appender.Events)
	}
	if appender.Events[0].ReasonCode != "first" || appender.Events[1].ReasonCode != "second" {
		t.Fatalf("Events = %#v, want them in call order", appender.Events)
	}
}

// TestFakeGovernanceAuditAppenderAccumulatesAcrossCalls covers the shape most
// consuming tests use: one appender wired into a handler for a whole request,
// asserted once at the end. Resetting per call would report the last event as
// though it were the only one.
func TestFakeGovernanceAuditAppenderAccumulatesAcrossCalls(t *testing.T) {
	t.Parallel()

	var appender querytestutil.FakeGovernanceAuditAppender

	for _, reason := range []string{"one", "two", "three"} {
		if err := appender.Append(context.Background(), []governanceaudit.Event{{ReasonCode: reason}}); err != nil {
			t.Fatalf("Append(%s) error = %v", reason, err)
		}
	}

	if len(appender.Events) != 3 {
		t.Fatalf("Events = %#v, want three accumulated events", appender.Events)
	}
}

// TestFakeGovernanceAuditAppenderZeroValueIsUsable matters because most callers
// construct it empty just to satisfy the audit port. The zero value has to
// record rather than panic on a nil slice.
func TestFakeGovernanceAuditAppenderZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var appender querytestutil.FakeGovernanceAuditAppender

	if appender.Events != nil {
		t.Fatalf("Events = %#v, want nil before any Append", appender.Events)
	}
	if err := appender.Append(context.Background(), nil); err != nil {
		t.Fatalf("Append(nil) error = %v", err)
	}
	if appender.Events != nil {
		t.Fatalf("Events = %#v, want nil after appending nothing", appender.Events)
	}
}
