// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerWorkItemIDIsNotInjective is a characterization of the #11203
// finding, not a statement of intent: reducerWorkItemID rewrites ':' and '/'
// to '_' and joins the parts with '_' without escaping, so two different
// (scope, generation, domain, entity key) tuples can map to one work_item_id.
// enqueueReducerBatch ends in ON CONFLICT (work_item_id) DO NOTHING, so the
// second intent of a colliding pair is coalesced into the first and never
// queued, with no error and no counter. Reaching it needs equal scope,
// generation, and domain plus entity keys that differ only in ':', '/', or '_'
// placement (for example GitLab group paths "grp/sub_proj" and "grp_sub/proj").
//
// Tracked in #11215. When the id is made injective, invert these assertions
// and delete this comment: the want-equal rows become want-distinct.
func TestReducerWorkItemIDIsNotInjective(t *testing.T) {
	t.Parallel()

	intent := func(scope, generation, entityKey string) runtime.ReducerIntent {
		return runtime.ReducerIntent{
			ScopeID:      scope,
			GenerationID: generation,
			Domain:       reducer.DomainWorkloadMaterialization,
			EntityKey:    entityKey,
		}
	}
	cases := []struct {
		name string
		a, b runtime.ReducerIntent
	}{
		{"colon versus slash in the entity key", intent("s", "g", "repo:a/b"), intent("s", "g", "repo:a:b")},
		{"slash versus underscore in the entity key", intent("s", "g", "grp/sub_proj"), intent("s", "g", "grp_sub/proj")},
		{"underscore moves across the scope and generation boundary", intent("s_1", "g", "e"), intent("s", "1_g", "e")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.a.ScopeID == tc.b.ScopeID && tc.a.GenerationID == tc.b.GenerationID && tc.a.EntityKey == tc.b.EntityKey {
				t.Fatalf("test bug: the two intents are identical")
			}
			idA, idB := reducerWorkItemID(tc.a), reducerWorkItemID(tc.b)
			if idA != idB {
				t.Fatalf("reducerWorkItemID distinct = %q vs %q; the id became injective, invert this test", idA, idB)
			}
		})
	}
}
