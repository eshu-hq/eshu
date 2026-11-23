// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestEnqueueRefusesAnIntentWhoseWorkItemIDAnotherIntentHolds is the #11215
// regression. reducerWorkItemID is not injective (TestReducerWorkItemIDIsNotInjective),
// and enqueue ends in ON CONFLICT (work_item_id) DO NOTHING, so a second, different
// intent that maps to a held id was coalesced into the first and never queued, with
// no error. The enqueue now resolves a shortfall: a held row whose stored scope,
// generation or domain differs from the incoming intent is a collision, and so is
// one whose stored entity key reproduces the row's own id yet differs from the
// incoming key; either is refused with a typed, non-retryable error. A stored key
// that does not reproduce the id says nothing, and a plain duplicate (the same
// intent already queued) is still a benign coalesce.
func TestEnqueueRefusesAnIntentWhoseWorkItemIDAnotherIntentHolds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 11, 0, 0, 0, time.UTC)
	// Two real key shapes: entity keys are "<domain literal>:" + scope id, and
	// scope ids differing only in '/' versus '_' share one work item id.
	held := runtime.ReducerIntent{
		ScopeID: "git-repository-scope:grp/sub_proj", GenerationID: "gen-1",
		Domain: reducer.DomainWorkloadMaterialization, EntityKey: "workload_materialization:git-repository-scope:grp/sub_proj",
	}
	incoming := runtime.ReducerIntent{
		ScopeID: "git-repository-scope:grp_sub/proj", GenerationID: "gen-1",
		Domain: reducer.DomainWorkloadMaterialization, EntityKey: "workload_materialization:git-repository-scope:grp_sub/proj",
	}
	if reducerWorkItemID(held) != reducerWorkItemID(incoming) {
		t.Fatalf("test bug: the two intents must share a work item id: %q vs %q", reducerWorkItemID(held), reducerWorkItemID(incoming))
	}

	for _, tc := range []struct {
		name      string
		stored    []any // the probe's held row, nil for none
		wantError bool
	}{
		{"a different intent holds the id", []any{reducerWorkItemID(held), held.ScopeID, held.GenerationID, string(held.Domain), held.EntityKey}, true},
		{"nothing differs: a benign duplicate", nil, false},
		// The probe returns every held id; Go decides. A stored payload with no
		// entity key (rows other writers insert, and older rows) says nothing
		// about the key, so the same scope, generation and domain is the same
		// intent: a benign coalesce, never a collision (#11270 wake regression).
		{"held row stores no entity key", []any{reducerWorkItemID(incoming), incoming.ScopeID, incoming.GenerationID, string(incoming.Domain), ""}, false},
		{"held row differs only by surrounding whitespace in the entity key", []any{reducerWorkItemID(incoming), incoming.ScopeID, incoming.GenerationID, string(incoming.Domain), "  " + incoming.EntityKey + "\t"}, false},
		{"held row stores a key that does not reproduce its id (a fixture or foreign writer)", []any{reducerWorkItemID(incoming), incoming.ScopeID, incoming.GenerationID, string(incoming.Domain), reducerWorkItemID(incoming)}, false},
		{"held row stores another key that reproduces the id (an entity key alias)", []any{reducerWorkItemID(incoming), incoming.ScopeID, incoming.GenerationID, string(incoming.Domain), strings.ReplaceAll(incoming.EntityKey, ":", "/")}, true},
		{"held row differs only by whitespace in the scope", []any{reducerWorkItemID(incoming), " " + incoming.ScopeID + " ", incoming.GenerationID, string(incoming.Domain), incoming.EntityKey}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rows [][]any
			if tc.stored != nil {
				rows = [][]any{tc.stored}
			}
			database := &fakeExecQueryer{
				execResults:      []sql.Result{rowsAffectedResult{rowsAffected: 0}}, // DO NOTHING admitted none
				heldWorkItemRows: rows,
			}
			queue := ReducerQueue{database: database, LeaseOwner: "reducer-1", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
			result, err := queue.Enqueue(context.Background(), []runtime.ReducerIntent{incoming})
			var collision *ReducerWorkItemCollisionError
			if tc.wantError {
				if !errors.As(err, &collision) {
					t.Fatalf("Enqueue() error = %v, want *ReducerWorkItemCollisionError", err)
				}
				if collision.Count != 1 || collision.Samples[0].IncomingScopeID != incoming.ScopeID ||
					collision.Samples[0].StoredScopeID != tc.stored[1] || collision.Retryable() {
					t.Fatalf("collision = %+v", collision)
				}
				return
			}
			if err != nil || result.Count != 0 {
				t.Fatalf("Enqueue() = %+v, %v, want a benign zero-count coalesce", result, err)
			}
		})
	}
}

// TestEnqueueComparesEveryIncomingIntentSharingAWorkItemID is the regression for
// a batch that holds two intents with one work item id. The probe used to keep
// only the last intent per id, so [Y, X] (X the intent already queued, Y a
// different intent aliasing its id) compared the held row with X alone and
// missed the collision that [X, Y] caught. A held row is a collision when ANY
// incoming intent that shares its id is a different intent, and it is counted
// once per held row, with the first differing intent as the sample.
func TestEnqueueComparesEveryIncomingIntentSharingAWorkItemID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 3, 0, 0, 0, time.UTC)
	same := runtime.ReducerIntent{
		ScopeID: "git-repository-scope:grp/sub_proj", GenerationID: "gen-1",
		Domain: reducer.DomainWorkloadMaterialization, EntityKey: "workload_materialization:git-repository-scope:grp/sub_proj",
	}
	otherA := runtime.ReducerIntent{
		ScopeID: "git-repository-scope:grp_sub/proj", GenerationID: "gen-1",
		Domain: reducer.DomainWorkloadMaterialization, EntityKey: "workload_materialization:git-repository-scope:grp_sub/proj",
	}
	otherB := runtime.ReducerIntent{
		ScopeID: "git-repository-scope:grp_sub_proj", GenerationID: "gen-1",
		Domain: reducer.DomainWorkloadMaterialization, EntityKey: "workload_materialization:git-repository-scope:grp_sub_proj",
	}
	spaced := same
	spaced.EntityKey = "  " + same.EntityKey + " "
	for _, other := range []runtime.ReducerIntent{otherA, otherB} {
		if reducerWorkItemID(same) != reducerWorkItemID(other) {
			t.Fatalf("test bug: the intents must share a work item id: %q vs %q", reducerWorkItemID(same), reducerWorkItemID(other))
		}
	}
	held := []any{reducerWorkItemID(same), same.ScopeID, same.GenerationID, string(same.Domain), same.EntityKey}

	for _, tc := range []struct {
		name      string
		batch     []runtime.ReducerIntent
		wantFirst string // the first differing incoming intent's scope, empty when benign
	}{
		{"queued intent first, aliasing intent second", []runtime.ReducerIntent{same, otherA}, otherA.ScopeID},
		{"aliasing intent first, queued intent second", []runtime.ReducerIntent{otherA, same}, otherA.ScopeID},
		{"two aliasing intents: one collision per held row", []runtime.ReducerIntent{otherA, otherB}, otherA.ScopeID},
		{"queued intent between two aliasing intents", []runtime.ReducerIntent{same, otherB, otherA}, otherB.ScopeID},
		{"the queued intent twice is a benign duplicate", []runtime.ReducerIntent{same, same}, ""},
		{"the queued intent and its whitespace spelling are benign", []runtime.ReducerIntent{spaced, same}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := &fakeExecQueryer{
				execResults:      []sql.Result{rowsAffectedResult{rowsAffected: 0}},
				heldWorkItemRows: [][]any{held},
			}
			queue := ReducerQueue{database: database, LeaseOwner: "reducer-1", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
			result, err := queue.Enqueue(context.Background(), tc.batch)
			if tc.wantFirst == "" {
				if err != nil || result.Count != 0 {
					t.Fatalf("Enqueue() = %+v, %v, want a benign zero-count coalesce", result, err)
				}
				return
			}
			var collision *ReducerWorkItemCollisionError
			if !errors.As(err, &collision) {
				t.Fatalf("Enqueue() error = %v, want *ReducerWorkItemCollisionError", err)
			}
			if collision.Count != 1 || len(collision.Samples) != 1 || collision.Samples[0].IncomingScopeID != tc.wantFirst || collision.Retryable() {
				t.Fatalf("collision = %+v, want one held row flagged by the first differing intent %q", collision, tc.wantFirst)
			}
		})
	}
}
