// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
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
// generation, domain or entity key differs from the incoming intent is a collision
// and is refused with a typed, non-retryable error; a plain duplicate (the same
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
					collision.Samples[0].StoredScopeID != held.ScopeID || collision.Retryable() {
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
