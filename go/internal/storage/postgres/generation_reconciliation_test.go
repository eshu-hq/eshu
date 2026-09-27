// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

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
		projected, latest, "pending", false,
	}}}}}
	store := IngestionStore{database: &projectedCommitTestDB{queryer: queryer}}

	got, err := store.FullReconcileState(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("FullReconcileState() error = %v", err)
	}
	want := scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: projected,
		HasLatestFull: true, LatestFullAt: latest,
		LatestFullStatus: scope.GenerationStatusPending, LatestFullProjected: false,
	}
	if got != want {
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
	} {
		if !strings.Contains(q, fragment) {
			t.Fatalf("fullReconcileStateQuery missing %q:\n%s", fragment, q)
		}
	}
}

func TestFullReconcileStateZeroWhenNoFullGeneration(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{{
		nil, nil, nil, false,
	}}}}}
	store := IngestionStore{database: &projectedCommitTestDB{queryer: queryer}}

	got, err := store.FullReconcileState(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("FullReconcileState() error = %v", err)
	}
	if got != (scope.FullReconcileState{}) {
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
	if got != (scope.FullReconcileState{}) {
		t.Fatalf("FullReconcileState() = %+v, want zero state", got)
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("blank scope must not query, got %d", len(queryer.queries))
	}
}
