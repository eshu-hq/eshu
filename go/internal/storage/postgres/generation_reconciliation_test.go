// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fullReconcileStatesEqual reports whether two FullReconcileState values
// carry the same sweep view. The recorded drop paths (#8185) keep the struct
// from being == comparable, so the comparison walks every field by
// reflection — a field added later is covered without updating this helper —
// with the paths compared by value so nil and empty compare equal.
func fullReconcileStatesEqual(got, want scope.FullReconcileState) bool {
	gotValue, wantValue := reflect.ValueOf(got), reflect.ValueOf(want)
	for i := 0; i < gotValue.NumField(); i++ {
		if gotValue.Type().Field(i).Name == "ActiveStreamDroppedPaths" {
			continue
		}
		if !reflect.DeepEqual(gotValue.Field(i).Interface(), wantValue.Field(i).Interface()) {
			return false
		}
	}
	return slices.Equal(got.ActiveStreamDroppedPaths, want.ActiveStreamDroppedPaths)
}

func TestUpsertScopeGenerationQueryPersistsIsDelta(t *testing.T) {
	t.Parallel()

	if !strings.Contains(upsertScopeGenerationQuery, "is_delta") {
		t.Fatalf("upsertScopeGenerationQuery must persist is_delta:\n%s", upsertScopeGenerationQuery)
	}
	if !strings.Contains(upsertScopeGenerationQuery, "is_delta = EXCLUDED.is_delta") {
		t.Fatalf("upsertScopeGenerationQuery must update is_delta on conflict:\n%s", upsertScopeGenerationQuery)
	}
}

func TestFullReconcileStateScansBothProbes(t *testing.T) {
	t.Parallel()

	projected := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 9, 27, 11, 30, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{{
		projected, projected, nil, nil, nil, latest, "pending", false, "sha-latest", true,
		"gen-active", false, "sha-active", "preparse:v1:x", int64(3), `["gone.yml", "a.py"]`, false,
	}}}}}
	store := IngestionStore{database: &projectedCommitTestDB{queryer: queryer}}

	got, err := store.FullReconcileState(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("FullReconcileState() error = %v", err)
	}
	want := scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: projected, LastStoredFullAt: projected,
		HasLatestFull: true, LatestFullAt: latest,
		LatestFullStatus: scope.GenerationStatusPending, LatestFullProjected: false,
		LatestFullSourceCommitSHA: "sha-latest", LatestFullWorkLive: true,
		ActiveGenerationID: "gen-active", ActiveCommitSHA: "sha-active",
		ActivePreparseIdentity: "preparse:v1:x", ActivePreparseSkips: 3,
		ActiveStreamDroppedPaths: []string{"a.py", "gone.yml"},
	}
	if !fullReconcileStatesEqual(got, want) {
		t.Fatalf("FullReconcileState() = %+v, want %+v", got, want)
	}

	q := queryer.queries[0]
	for _, fragment := range []string{
		"projected.activated_at IS NOT NULL",
		"projected.is_delta = false",
		"'active', 'completed', 'superseded'",
		"attempt.is_delta = false",
		"LEFT JOIN LATERAL",
		"ORDER BY attempt.ingested_at DESC, attempt.generation_id DESC",
		"pointed.generation_id = pointer.active_generation_id",
		"payload ->> 'preparse_identity'",
		"payload ->> 'stream_dropped_paths'",
	} {
		if !strings.Contains(q, fragment) {
			t.Fatalf("fullReconcileStateQuery missing %q:\n%s", fragment, q)
		}
	}
}

func TestFullReconcileStateZeroWhenNoFullGeneration(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{{
		nil, nil, nil, nil, nil, nil, nil, false, "", false,
		"", false, "", "", int64(0), "[]", false,
	}}}}}
	store := IngestionStore{database: &projectedCommitTestDB{queryer: queryer}}

	got, err := store.FullReconcileState(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("FullReconcileState() error = %v", err)
	}
	if !fullReconcileStatesEqual(got, scope.FullReconcileState{}) {
		t.Fatalf("FullReconcileState() = %+v, want zero state", got)
	}
}

func TestFullReconcileStateBlankScopeDoesNotQuery(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{}
	store := IngestionStore{database: &projectedCommitTestDB{queryer: queryer}}

	got, err := store.FullReconcileState(context.Background(), "  ")
	if err != nil {
		t.Fatalf("FullReconcileState() error = %v", err)
	}
	if !fullReconcileStatesEqual(got, scope.FullReconcileState{}) {
		t.Fatalf("FullReconcileState() = %+v, want zero state", got)
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("blank scope must not query, got %d", len(queryer.queries))
	}
}
