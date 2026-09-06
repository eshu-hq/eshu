// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

type governanceAuditWarnLine struct {
	Level  string `json:"level"`
	Msg    string `json:"msg"`
	Field  string `json:"field"`
	Rows   int    `json:"rows"`
	Values string `json:"values"`
}

func decodeGovernanceAuditWarnLines(t *testing.T, logs *bytes.Buffer) []governanceAuditWarnLine {
	t.Helper()
	var lines []governanceAuditWarnLine
	for _, raw := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if raw == "" {
			continue
		}
		var line governanceAuditWarnLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("decode log line %q: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return lines
}

// TestGovernanceAuditStoreListWarnsOncePerUnknownEnumField pins the operator
// signal for #6574: a List page holding values this build's registry lacks
// logs one WARN per affected field, naming the field, how many rows on the
// page carried an unknown value in it, and the distinct shape-checked
// values, and never one line per row. A page of known values logs nothing.
func TestGovernanceAuditStoreListWarnsOncePerUnknownEnumField(t *testing.T) {
	t.Parallel()

	known := governanceAuditScanRow(
		string(governanceaudit.EventTypeReadAuthorization),
		string(governanceaudit.ActorClassScopedToken),
		string(governanceaudit.ScopeClassRepository),
		string(governanceaudit.DecisionDenied),
	)
	futureClass := func() []any {
		row := append([]any(nil), known...)
		row[1] = "future_class"
		return row
	}
	otherClassAndEvent := append([]any(nil), known...)
	otherClassAndEvent[0] = "future_event"
	otherClassAndEvent[1] = "other_class"

	var logs bytes.Buffer
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{futureClass(), known, futureClass(), otherClassAndEvent}},
		{rows: [][]any{known, known}},
	}}
	store := NewGovernanceAuditStore(db).WithLogger(slog.New(slog.NewJSONHandler(&logs, nil)))
	query := GovernanceAuditQuery{OperatorAuthorized: true, Limit: 25}

	events, err := store.List(context.Background(), query)
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}
	if got, want := len(events), 4; got != want {
		t.Fatalf("events len = %d, want %d", got, want)
	}
	want := []governanceAuditWarnLine{
		{Level: "WARN", Msg: governanceAuditUnknownEnumMessage, Field: "event_type", Rows: 1, Values: "future_event"},
		{Level: "WARN", Msg: governanceAuditUnknownEnumMessage, Field: "actor_class", Rows: 3, Values: "future_class,other_class"},
	}
	got := decodeGovernanceAuditWarnLines(t, &logs)
	if len(got) != len(want) {
		t.Fatalf("warn lines = %d, want %d:\n%s", len(got), len(want), logs.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("warn line %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	logs.Reset()
	if _, err := store.List(context.Background(), query); err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("List over known values logged %q, want nothing", logs.String())
	}
}

// TestGovernanceAuditStoreListWarnsThroughDefaultLoggerWhenUnset: a store
// built without WithLogger still emits the warn, through slog.Default, so the
// production wiring in cmd/api does not have to change for the signal to
// exist.
func TestGovernanceAuditStoreListWarnsThroughDefaultLoggerWhenUnset(t *testing.T) {
	// Not parallel: swaps the process-wide default logger.
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	row := governanceAuditScanRow(
		string(governanceaudit.EventTypeReadAuthorization),
		"future_class",
		string(governanceaudit.ScopeClassRepository),
		string(governanceaudit.DecisionDenied),
	)
	if _, err := listGovernanceAuditRows(t, row); err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}
	got := decodeGovernanceAuditWarnLines(t, &logs)
	if len(got) != 1 || got[0].Field != "actor_class" || got[0].Rows != 1 || got[0].Values != "future_class" {
		t.Fatalf("default-logger warn lines = %+v, want one actor_class line", got)
	}
}
