// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestUnsafeReplayTargetsQueryShapeAndScan(t *testing.T) {
	t.Parallel()

	database := &recordingAdminExecQueryer{rows: &testutil.ScriptedRows{Data: [][]any{
		{"wi-a", "projection_bug"},
		{"wi-b", "resource_exhausted"},
	}}}
	store := &postgresStore{database: database, now: func() time.Time { return time.Unix(0, 0).UTC() }}

	got, err := store.UnsafeReplayTargets(context.Background(), admin.UnsafeReplayTargetFilter{
		WorkItemIDs:          []string{"wi-a", "wi-b", "wi-c"},
		ScopeID:              "scope-1",
		Stage:                "projector",
		UnsafeFailureClasses: []string{"projection_bug", "resource_exhausted"},
	})
	if err != nil {
		t.Fatalf("UnsafeReplayTargets() error = %v", err)
	}
	if len(got) != 2 || got[0].WorkItemID != "wi-a" || got[0].FailureClass != "projection_bug" || got[1].FailureClass != "resource_exhausted" {
		t.Fatalf("targets = %+v, want wi-a/projection_bug and wi-b/resource_exhausted", got)
	}
	for _, want := range []string{
		"status IN ('dead_letter', 'failed')",
		"work_item_id = ANY($1)",
		"failure_class = ANY($2)",
		"scope_id = $3",
		"stage = $4",
	} {
		if !strings.Contains(database.query, want) {
			t.Fatalf("query missing %q:\n%s", want, database.query)
		}
	}
	if got, want := maxPlaceholderIndex(database.query), len(database.queryArgs); got != want {
		t.Fatalf("max placeholder index = %d, want %d", got, want)
	}
}

func TestUnsafeReplayTargetsOmitsOptionalPredicatesAndSkipsEmptyInput(t *testing.T) {
	t.Parallel()

	database := &recordingAdminExecQueryer{rows: &testutil.ScriptedRows{}}
	store := &postgresStore{database: database}

	if _, err := store.UnsafeReplayTargets(context.Background(), admin.UnsafeReplayTargetFilter{
		WorkItemIDs:          []string{"wi-a"},
		UnsafeFailureClasses: []string{"projection_bug"},
	}); err != nil {
		t.Fatalf("UnsafeReplayTargets() error = %v", err)
	}
	if strings.Contains(database.query, "scope_id") || strings.Contains(database.query, "stage =") {
		t.Fatalf("unset selectors must not add predicates:\n%s", database.query)
	}
	if got := len(database.queryArgs); got != 2 {
		t.Fatalf("len(queryArgs) = %d, want 2", got)
	}

	database.query = ""
	got, err := store.UnsafeReplayTargets(context.Background(), admin.UnsafeReplayTargetFilter{})
	if err != nil || got != nil || database.query != "" {
		t.Fatalf("empty filter must not query: got=%v err=%v query=%q", got, err, database.query)
	}
}
