// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// projectedCommitTestDB adapts a read-only fakeQueryer into the ExecQueryer
// surface IngestionStore requires. Writes are not exercised by the
// last-projected-commit reader, so ExecContext is a hard failure.
type projectedCommitTestDB struct {
	queryer *fakeQueryer
}

func (database *projectedCommitTestDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext not implemented in test stub")
}

func (database *projectedCommitTestDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return database.queryer.QueryContext(ctx, query, args...)
}

func TestUpsertScopeGenerationQueryPersistsSourceCommitSHA(t *testing.T) {
	t.Parallel()

	if !strings.Contains(upsertScopeGenerationQuery, "source_commit_sha") {
		t.Fatalf("upsertScopeGenerationQuery must persist source_commit_sha:\n%s", upsertScopeGenerationQuery)
	}
	if !strings.Contains(upsertScopeGenerationQuery, "source_commit_sha = EXCLUDED.source_commit_sha") {
		t.Fatalf("upsertScopeGenerationQuery must update source_commit_sha on conflict:\n%s", upsertScopeGenerationQuery)
	}
}

func TestDeltaSyncBaselineReadsActiveAndNewestFull(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{{"c0ffee", true, "f00d"}}}}}
	store := IngestionStore{database: &projectedCommitTestDB{queryer: queryer}}

	got, err := store.DeltaSyncBaseline(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("DeltaSyncBaseline() error = %v", err)
	}
	want := scope.DeltaSyncBaseline{ActiveCommitSHA: "c0ffee", ActiveIsDelta: true, NewestActivatedFullCommitSHA: "f00d"}
	if got != want {
		t.Fatalf("DeltaSyncBaseline() = %+v, want %+v", got, want)
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("queries = %d, want 1 (one statement, one snapshot)", len(queryer.queries))
	}
	q := queryer.queries[0]
	// The fence commit is the active generation only (#7317): the same
	// predicate the #7319 fence reads, never a superseded, pending, or failed
	// row. The union base is the newest activated full (#8780), the same rule
	// as fullReconcileStateQuery's projected_full and retention's delta_base.
	for _, want := range []string{
		activeGenerationPredicate,
		"projected.is_delta = false",
		"projected.activated_at IS NOT NULL",
		"projected.status IN ('active', 'completed', 'superseded')",
		"ORDER BY projected.ingested_at DESC, projected.generation_id DESC",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("deltaSyncBaselineQuery missing %q:\n%s", want, q)
		}
	}
}

func TestDeltaSyncBaselineEmptyWhenNoGeneration(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{{"", false, ""}}}}}
	store := IngestionStore{database: &projectedCommitTestDB{queryer: queryer}}

	got, err := store.DeltaSyncBaseline(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("DeltaSyncBaseline() error = %v", err)
	}
	if got != (scope.DeltaSyncBaseline{}) {
		t.Fatalf("DeltaSyncBaseline() = %+v, want zero", got)
	}
}

func TestDeltaSyncBaselineBlankScopeReturnsEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{}
	store := IngestionStore{database: &projectedCommitTestDB{queryer: queryer}}

	got, err := store.DeltaSyncBaseline(context.Background(), "  ")
	if err != nil {
		t.Fatalf("DeltaSyncBaseline() error = %v", err)
	}
	if got != (scope.DeltaSyncBaseline{}) {
		t.Fatalf("DeltaSyncBaseline() = %+v, want zero", got)
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("blank scope must not query, got %d queries", len(queryer.queries))
	}
}
