// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// governanceAuditScanRow builds one stored governance_audit_events row in the
// column order scanGovernanceAuditEvent reads, so a test can vary a single
// enum column and drive the production List path over it.
func governanceAuditScanRow(eventType, actorClass, scopeClass, decision string) []any {
	return []any{
		eventType,
		actorClass,
		sql.NullString{String: "sha256:aaaaaaaaaaaaaaaa", Valid: true},
		sql.NullString{},
		scopeClass,
		sql.NullString{String: "sha256:bbbbbbbbbbbbbbbb", Valid: true},
		decision,
		"subject_scope_missing",
		sql.NullString{String: "corr:read-denied-1", Valid: true},
		sql.NullString{},
		governanceAuditStoreTestTime(),
		sql.NullString{}, // tenant_id
		sql.NullString{}, // workspace_id
	}
}

func listGovernanceAuditRows(t *testing.T, rows ...[]any) ([]governanceaudit.Event, error) {
	t.Helper()
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: rows}}}
	return NewGovernanceAuditStore(db).List(context.Background(), GovernanceAuditQuery{
		OperatorAuthorized: true,
		Limit:              25,
	})
}

// TestGovernanceAuditStoreListKeepsUnknownEnumValuesVerbatim is the #6574
// regression: a row written by a newer build with a class this build does not
// know must come back with the stored value, not fail the whole page. Before
// the fix the scanner ran every row through the closed write-path validator
// and List returned `governance audit field "actor_class" is invalid`.
func TestGovernanceAuditStoreListKeepsUnknownEnumValuesVerbatim(t *testing.T) {
	t.Parallel()

	known := governanceAuditScanRow(
		string(governanceaudit.EventTypeReadAuthorization),
		string(governanceaudit.ActorClassScopedToken),
		string(governanceaudit.ScopeClassRepository),
		string(governanceaudit.DecisionDenied),
	)
	cases := []struct {
		name   string
		column int
		value  string
		read   func(governanceaudit.Event) string
	}{
		{name: "actor_class", column: 1, value: "future_class", read: func(e governanceaudit.Event) string { return string(e.ActorClass) }},
		{name: "event_type", column: 0, value: "future_event", read: func(e governanceaudit.Event) string { return string(e.Type) }},
		{name: "scope_class", column: 4, value: "future_scope", read: func(e governanceaudit.Event) string { return string(e.ScopeClass) }},
		{name: "decision", column: 6, value: "future_decision", read: func(e governanceaudit.Event) string { return string(e.Decision) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := append([]any(nil), known...)
			row[tc.column] = tc.value
			events, err := listGovernanceAuditRows(t, row, known)
			if err != nil {
				t.Fatalf("List error = %v, want nil for unknown %s", err, tc.name)
			}
			if got, want := len(events), 2; got != want {
				t.Fatalf("events len = %d, want %d", got, want)
			}
			if got := tc.read(events[0]); got != tc.value {
				t.Fatalf("%s = %q, want stored value %q", tc.name, got, tc.value)
			}
		})
	}
}

// TestGovernanceAuditStoreListStillRejectsUnsafeStoredRows pins the guards the
// tolerant reader keeps: an unknown class must still look like a bounded enum
// token, and hash-shaped fields are still checked, so a corrupt or hand-edited
// row cannot leak a raw value through the read path.
func TestGovernanceAuditStoreListStillRejectsUnsafeStoredRows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		mutate    func(row []any)
		wantField string
	}{
		{
			name:      "actor class that is not a bounded token",
			mutate:    func(row []any) { row[1] = "Future Class: https://example.invalid" },
			wantField: "actor_class",
		},
		{
			name:      "actor id hash that is not a sha256 hash",
			mutate:    func(row []any) { row[2] = sql.NullString{String: "alice@example.invalid", Valid: true} },
			wantField: "actor_id_hash",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := governanceAuditScanRow(
				string(governanceaudit.EventTypeReadAuthorization),
				"future_class",
				string(governanceaudit.ScopeClassRepository),
				string(governanceaudit.DecisionDenied),
			)
			tc.mutate(row)
			_, err := listGovernanceAuditRows(t, row)
			if err == nil {
				t.Fatalf("List error = nil, want %q rejection", tc.wantField)
			}
			if !strings.Contains(err.Error(), tc.wantField) {
				t.Fatalf("List error = %q, want it to name %q", err, tc.wantField)
			}
			for _, raw := range []string{"example.invalid", "Future Class"} {
				if strings.Contains(err.Error(), raw) {
					t.Fatalf("List error echoes raw value %q: %v", raw, err)
				}
			}
		})
	}
}

// TestGovernanceAuditStoreAppendRejectsUnknownActorClass pins the write side of
// the #6574 split: the reader tolerates a class it does not know, but a
// producer on this build still may not emit one.
func TestGovernanceAuditStoreAppendRejectsUnknownActorClass(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	err := NewGovernanceAuditStore(db).Append(context.Background(), []governanceaudit.Event{{
		Type:        governanceaudit.EventTypeReadAuthorization,
		ActorClass:  "future_class",
		ActorIDHash: "sha256:aaaaaaaaaaaaaaaa",
		ScopeClass:  governanceaudit.ScopeClassRepository,
		Decision:    governanceaudit.DecisionDenied,
		ReasonCode:  "subject_scope_missing",
		OccurredAt:  governanceAuditStoreTestTime(),
	}})
	if err == nil || !strings.Contains(err.Error(), "actor_class") {
		t.Fatalf("Append error = %v, want actor_class rejection", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("Append executed %d statements, want 0", len(db.execs))
	}
}
