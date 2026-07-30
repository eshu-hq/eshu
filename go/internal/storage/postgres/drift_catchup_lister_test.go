// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestIngestionStoreListActiveStateSnapshotScopesReturnsBoundedPendingScopes
// proves the issue #5593 lister: it scans the SAME active state_snapshot:*
// set listActiveStateSnapshotScopes (bootstrap Phase 3.5) scans, translated
// into projector.PendingConfigStateDriftScope, bounded by the
// caller-supplied limit via a LIMIT clause -- not the unbounded query Phase
// 3.5 uses, because this lister runs on a recurring interval from the
// reducer's catch-up sweep and must not turn every tick into an unbounded
// scan on a large corpus. Also proves the query filters by scope_kind
// (bound as a parameter, not a LIKE-prefix on scope_id) so it can be
// serviced by ingestion_scopes_active_state_snapshot_idx (migration 087) --
// see drift_catchup_lister.go's doc comment and
// docs/internal/evidence/5593-config-state-drift-catchup-lister-query.md for
// why the LIKE shape was rejected.
func TestIngestionStoreListActiveStateSnapshotScopesReturnsBoundedPendingScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"state_snapshot:s3:hash-1", "gen-state-1"},
				{"state_snapshot:s3:hash-2", "gen-state-2"},
			}},
		},
	}
	store := NewIngestionStore(db)

	scopes, err := store.ListActiveStateSnapshotScopes(context.Background(), 500)
	if err != nil {
		t.Fatalf("ListActiveStateSnapshotScopes() error = %v, want nil", err)
	}

	want := []projector.PendingConfigStateDriftScope{
		{ScopeID: "state_snapshot:s3:hash-1", GenerationID: "gen-state-1"},
		{ScopeID: "state_snapshot:s3:hash-2", GenerationID: "gen-state-2"},
	}
	if len(scopes) != len(want) {
		t.Fatalf("ListActiveStateSnapshotScopes() = %#v, want %#v", scopes, want)
	}
	for i := range want {
		if scopes[i] != want[i] {
			t.Fatalf("scopes[%d] = %#v, want %#v", i, scopes[i], want[i])
		}
	}

	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	if got := db.queries[0].args; len(got) != 2 || got[0] != "state_snapshot" || got[1] != 500 {
		t.Fatalf("query args = %#v, want [\"state_snapshot\" 500] (scope_kind bind, then LIMIT bound)", got)
	}
	if strings.Contains(db.queries[0].query, "LIKE") {
		t.Fatalf("query = %q, want no LIKE predicate -- must filter on the indexed scope_kind equality, not a scope_id prefix scan (issue #5593 perf regression)", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "scope_kind") {
		t.Fatalf("query = %q, want a scope_kind predicate", db.queries[0].query)
	}
}

// TestIngestionStoreListActiveStateSnapshotScopesDefaultsNonPositiveLimit
// proves a caller-supplied zero/negative limit falls back to
// defaultConfigStateDriftCatchUpLimit rather than sending an invalid or
// unbounded LIMIT to Postgres.
func TestIngestionStoreListActiveStateSnapshotScopesDefaultsNonPositiveLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	store := NewIngestionStore(db)

	if _, err := store.ListActiveStateSnapshotScopes(context.Background(), 0); err != nil {
		t.Fatalf("ListActiveStateSnapshotScopes() error = %v, want nil", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	if got := db.queries[0].args; len(got) != 2 || got[0] != "state_snapshot" || got[1] != defaultCatchUpListLimit {
		t.Fatalf("query args = %#v, want [\"state_snapshot\" %d] (default limit)", got, defaultCatchUpListLimit)
	}
}

// TestIngestionStoreListActiveStateSnapshotScopesRequiresDatabase proves the
// lister fails closed instead of panicking when constructed without a DB.
func TestIngestionStoreListActiveStateSnapshotScopesRequiresDatabase(t *testing.T) {
	t.Parallel()

	var store IngestionStore
	if _, err := store.ListActiveStateSnapshotScopes(context.Background(), 500); err == nil {
		t.Fatal("nil DB: error = nil, want non-nil")
	}
}

// TestIngestionStoreListActiveStateSnapshotScopesWrapsScanError proves a scan
// failure is surfaced, not swallowed.
func TestIngestionStoreListActiveStateSnapshotScopesWrapsScanError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{err: errors.New("scan boom")}},
	}
	store := NewIngestionStore(db)

	if _, err := store.ListActiveStateSnapshotScopes(context.Background(), 500); err == nil {
		t.Fatal("error = nil, want non-nil")
	}
}
