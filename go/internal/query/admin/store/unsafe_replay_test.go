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

func TestUnsafeReplayTargetsQueryAppliesFailureClassSelector(t *testing.T) {
	t.Parallel()

	database := &recordingAdminExecQueryer{rows: &testutil.ScriptedRows{}}
	store := &postgresStore{database: database}
	if _, err := store.UnsafeReplayTargets(context.Background(), admin.UnsafeReplayTargetFilter{
		WorkItemIDs:          []string{"wi-a"},
		FailureClass:         "transient_error",
		UnsafeFailureClasses: []string{"projection_bug"},
	}); err != nil {
		t.Fatalf("UnsafeReplayTargets() error = %v", err)
	}
	if !strings.Contains(database.query, "AND failure_class = $3") {
		t.Fatalf("query missing failure_class selector predicate:\n%s", database.query)
	}
	if got, want := maxPlaceholderIndex(database.query), len(database.queryArgs); got != want {
		t.Fatalf("max placeholder index = %d, want %d", got, want)
	}
}

// TestReplayFencesSupersededProjectorGenerationsButDeadLetterDoesNot pins
// which mutation carries the #7130 fence: replay must skip projector rows on
// superseded generations, while an operator dead-letter still reaches them.
func TestReplayFencesSupersededProjectorGenerationsButDeadLetterDoesNot(t *testing.T) {
	t.Parallel()

	replay, _ := buildMutatingWorkItemsQuery(nil, "", "projector", "", 10, 1, true, "SET status = 'pending'\n")
	if !strings.Contains(replay, "AND NOT (stage = 'projector' AND EXISTS (") ||
		!strings.Contains(replay, "fenced_generation.status = 'superseded'") {
		t.Fatalf("replay query lacks the superseded-generation fence:\n%s", replay)
	}
	if strings.Index(replay, "fenced_generation") > strings.Index(replay, "ORDER BY updated_at DESC") {
		t.Fatalf("fence must sit in the selection before its LIMIT:\n%s", replay)
	}
	deadLetter, _ := buildMutatingWorkItemsQuery(nil, "", "projector", "", 10, 2, false, "SET status = 'dead_letter'\n")
	if strings.Contains(deadLetter, "fenced_generation") {
		t.Fatalf("dead-letter query must not carry the replay fence:\n%s", deadLetter)
	}
}

func TestSupersededReplayTargetsQueryShapeAndScan(t *testing.T) {
	t.Parallel()

	database := &recordingAdminExecQueryer{rows: &testutil.ScriptedRows{Data: [][]any{
		{"wi-a", "gen-old"},
	}}}
	store := &postgresStore{database: database, now: func() time.Time { return time.Unix(0, 0).UTC() }}

	got, err := store.SupersededReplayTargets(context.Background(), admin.UnsafeReplayTargetFilter{
		WorkItemIDs: []string{"wi-a", "wi-b"},
		ScopeID:     "scope-1",
		Stage:       "projector",
	})
	if err != nil {
		t.Fatalf("SupersededReplayTargets() error = %v", err)
	}
	if len(got) != 1 || got[0].WorkItemID != "wi-a" || got[0].GenerationID != "gen-old" {
		t.Fatalf("targets = %+v, want wi-a on gen-old", got)
	}
	for _, want := range []string{
		"work.status IN ('dead_letter', 'failed')",
		"work.work_item_id = ANY($1)",
		"work.stage = 'projector'",
		"generation.status = 'superseded'",
		"work.scope_id = $2",
		"work.stage = $3",
	} {
		if !strings.Contains(database.query, want) {
			t.Fatalf("superseded read missing %q:\n%s", want, database.query)
		}
	}
}
