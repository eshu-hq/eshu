// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceaudit_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

func storedEventFixture() governanceaudit.Event {
	return governanceaudit.Event{
		Type:        governanceaudit.EventTypeReadAuthorization,
		ActorClass:  governanceaudit.ActorClassScopedToken,
		ActorIDHash: "sha256:aaaaaaaaaaaaaaaa",
		ScopeClass:  governanceaudit.ScopeClassRepository,
		Decision:    governanceaudit.DecisionDenied,
		ReasonCode:  "subject_scope_missing",
		OccurredAt:  time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
}

// TestNormalizeStoredEventKeepsUnknownEnumsThatWriteRejects is the #6574
// split in one place: the same event fails NormalizeEvent (write path) and
// passes NormalizeStoredEvent (read path) with the stored value untouched, for
// each of the four closed enums.
func TestNormalizeStoredEventKeepsUnknownEnumsThatWriteRejects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		field  string
		mutate func(*governanceaudit.Event)
		read   func(governanceaudit.Event) string
	}{
		{
			field:  "type",
			mutate: func(e *governanceaudit.Event) { e.Type = " future_event " },
			read:   func(e governanceaudit.Event) string { return string(e.Type) },
		},
		{
			field:  "actor_class",
			mutate: func(e *governanceaudit.Event) { e.ActorClass = " future_class " },
			read:   func(e governanceaudit.Event) string { return string(e.ActorClass) },
		},
		{
			field:  "scope_class",
			mutate: func(e *governanceaudit.Event) { e.ScopeClass = " future_scope " },
			read:   func(e governanceaudit.Event) string { return string(e.ScopeClass) },
		},
		{
			field:  "decision",
			mutate: func(e *governanceaudit.Event) { e.Decision = " future_decision " },
			read:   func(e governanceaudit.Event) string { return string(e.Decision) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			event := storedEventFixture()
			tc.mutate(&event)

			if _, err := governanceaudit.NormalizeEvent(event); err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("NormalizeEvent error = %v, want %q rejection on the write path", err, tc.field)
			}
			stored, err := governanceaudit.NormalizeStoredEvent(event)
			if err != nil {
				t.Fatalf("NormalizeStoredEvent error = %v, want nil", err)
			}
			want := strings.TrimSpace(tc.read(event))
			if got := tc.read(stored); got != want {
				t.Fatalf("%s = %q, want trimmed stored value %q", tc.field, got, want)
			}
		})
	}
}

// TestNormalizeStoredEventUnknownActorClassSkipsIdentityRule: only the build
// that accepted a class knows whether it carries an identity, so the reader
// does not demand a hash for a class it cannot classify. Known classes keep
// the rule.
func TestNormalizeStoredEventUnknownActorClassSkipsIdentityRule(t *testing.T) {
	t.Parallel()

	event := storedEventFixture()
	event.ActorClass = "future_class"
	event.ActorIDHash = ""
	if _, err := governanceaudit.NormalizeStoredEvent(event); err != nil {
		t.Fatalf("NormalizeStoredEvent error = %v, want nil for unknown class without identity", err)
	}

	event.ActorClass = governanceaudit.ActorClassScopedToken
	_, err := governanceaudit.NormalizeStoredEvent(event)
	if err == nil || !strings.Contains(err.Error(), "actor_identity") {
		t.Fatalf("NormalizeStoredEvent error = %v, want actor_identity rejection for known class", err)
	}
}

// TestNormalizeStoredEventStillRejectsUnsafeValues pins what the tolerant
// reader does not loosen: an unknown enum must be a bounded lowercase token,
// and every hash, token, and reason-code guard still runs.
func TestNormalizeStoredEventStillRejectsUnsafeValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		field  string
		mutate func(*governanceaudit.Event)
	}{
		{name: "empty actor class", field: "actor_class", mutate: func(e *governanceaudit.Event) { e.ActorClass = "" }},
		{name: "actor class with url", field: "actor_class", mutate: func(e *governanceaudit.Event) { e.ActorClass = "https://example.invalid/x" }},
		{name: "actor class with upper case", field: "actor_class", mutate: func(e *governanceaudit.Event) { e.ActorClass = "FutureClass" }},
		{name: "actor class over 64 bytes", field: "actor_class", mutate: func(e *governanceaudit.Event) { e.ActorClass = governanceaudit.ActorClass(strings.Repeat("a", 65)) }},
		{name: "type with space", field: "type", mutate: func(e *governanceaudit.Event) { e.Type = "future event" }},
		{name: "scope class with dash", field: "scope_class", mutate: func(e *governanceaudit.Event) { e.ScopeClass = "future-scope" }},
		{name: "decision with colon", field: "decision", mutate: func(e *governanceaudit.Event) { e.Decision = "denied:reason" }},
		{name: "raw email as actor hash", field: "actor_id_hash", mutate: func(e *governanceaudit.Event) { e.ActorIDHash = "alice@example.invalid" }},
		{name: "raw url as correlation id", field: "correlation_id", mutate: func(e *governanceaudit.Event) { e.CorrelationID = "https://example.invalid/private" }},
		{name: "reason code with path", field: "reason_code", mutate: func(e *governanceaudit.Event) { e.ReasonCode = "/etc/passwd" }},
		{name: "zero occurred at", field: "occurred_at", mutate: func(e *governanceaudit.Event) { e.OccurredAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			event := storedEventFixture()
			tc.mutate(&event)
			_, err := governanceaudit.NormalizeStoredEvent(event)
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("NormalizeStoredEvent error = %v, want %q rejection", err, tc.field)
			}
			for _, raw := range []string{"example.invalid", "FutureClass", "/etc/passwd"} {
				if strings.Contains(err.Error(), raw) {
					t.Fatalf("error echoes raw value %q: %v", raw, err)
				}
			}
		})
	}
}

// TestAggregateCountsUnknownClassAsPlainString: the in-memory summary matches
// the SQL summary, which groups the stored column without checking it against
// the registry, so an unknown class becomes its own count bucket.
func TestAggregateCountsUnknownClassAsPlainString(t *testing.T) {
	t.Parallel()

	known := storedEventFixture()
	unknown := storedEventFixture()
	unknown.ActorClass = "future_class"
	unknown.OccurredAt = known.OccurredAt.Add(time.Minute)

	summary, err := governanceaudit.Aggregate([]governanceaudit.Event{known, unknown})
	if err != nil {
		t.Fatalf("Aggregate() error = %v, want nil", err)
	}
	if got, want := summary.Total, 2; got != want {
		t.Fatalf("Total = %d, want %d", got, want)
	}
	if got, want := summary.Denied, 2; got != want {
		t.Fatalf("Denied = %d, want %d", got, want)
	}
	requireCount(t, summary.ActorClassCounts, "future_class", 1)
	requireCount(t, summary.ActorClassCounts, string(governanceaudit.ActorClassScopedToken), 1)
	if !summary.LastOccurredAt.Equal(unknown.OccurredAt) {
		t.Fatalf("LastOccurredAt = %v, want %v", summary.LastOccurredAt, unknown.OccurredAt)
	}

	unknown.ActorIDHash = "alice@example.invalid"
	if _, err := governanceaudit.Aggregate([]governanceaudit.Event{unknown}); err == nil {
		t.Fatal("Aggregate() error = nil, want rejection of an unsafe hash")
	}
}
