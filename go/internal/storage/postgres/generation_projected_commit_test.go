// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// projectedCommitTestDB adapts a read-only fakeQueryer into the ExecQueryer
// surface IngestionStore requires. Writes are not exercised by the
// last-projected-commit reader, so ExecContext is a hard failure.
type projectedCommitTestDB struct {
	queryer *fakeQueryer
}

func (db *projectedCommitTestDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext not implemented in test stub")
}

func (db *projectedCommitTestDB) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return db.queryer.QueryContext(ctx, query, args...)
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

func TestLastProjectedCommitSHAReturnsLatestProjectedSHA(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{{"c0ffee"}}}}}
	store := IngestionStore{db: &projectedCommitTestDB{queryer: queryer}}

	sha, err := store.LastProjectedCommitSHA(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("LastProjectedCommitSHA() error = %v", err)
	}
	if sha != "c0ffee" {
		t.Fatalf("sha = %q, want %q", sha, "c0ffee")
	}

	if len(queryer.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queryer.queries))
	}
	q := queryer.queries[0]
	for _, want := range []string{
		"source_commit_sha",
		"scope_id = $1",
		"'active', 'completed', 'superseded'",
		"source_commit_sha IS NOT NULL",
		"LIMIT 1",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("lastProjectedCommitSHAQuery missing %q:\n%s", want, q)
		}
	}
	// The baseline must only derive from generations that reached a projected
	// state. A pending or failed generation never projected, so including it
	// would silently advance the baseline past unprojected changes.
	if strings.Contains(q, "'pending'") || strings.Contains(q, "'failed'") {
		t.Fatalf("lastProjectedCommitSHAQuery must exclude pending/failed:\n%s", q)
	}
}

func TestLastProjectedCommitSHAEmptyWhenNoProjectedGeneration(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}}}
	store := IngestionStore{db: &projectedCommitTestDB{queryer: queryer}}

	sha, err := store.LastProjectedCommitSHA(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("LastProjectedCommitSHA() error = %v", err)
	}
	if sha != "" {
		t.Fatalf("sha = %q, want empty", sha)
	}
}

func TestLastProjectedCommitSHABlankScopeReturnsEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{}
	store := IngestionStore{db: &projectedCommitTestDB{queryer: queryer}}

	sha, err := store.LastProjectedCommitSHA(context.Background(), "  ")
	if err != nil {
		t.Fatalf("LastProjectedCommitSHA() error = %v", err)
	}
	if sha != "" {
		t.Fatalf("sha = %q, want empty", sha)
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("blank scope must not query, got %d queries", len(queryer.queries))
	}
}
