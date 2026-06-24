// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func TestContentWriterUpsertsRepositoryRefs(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2026, 6, 1, 9, 5, 0, 0, time.UTC) }
	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	result, err := writer.Write(context.Background(), content.Materialization{
		RepoID:       "repo-1",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryRefs: []content.RepositoryRef{
			{
				Name:       "main",
				Kind:       "branch",
				HeadSHA:    "abc123",
				Default:    true,
				ObservedAt: observedAt,
			},
			{
				Name:       "release",
				Kind:       "branch",
				HeadSHA:    "def456",
				ObservedAt: observedAt,
			},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got, want := result.RepositoryRefCount, 2; got != want {
		t.Fatalf("RepositoryRefCount = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d (stale ref delete + ref upsert)", got, want)
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM repository_refs") {
		t.Fatalf("first query = %q, want repository_refs stale delete", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "INSERT INTO repository_refs") {
		t.Fatalf("second query = %q, want repository_refs upsert", db.execs[1].query)
	}
	if !fakeExecArgsContain(db.execs[1].args, "main") || !fakeExecArgsContain(db.execs[1].args, "def456") {
		t.Fatalf("repository ref upsert args = %#v, want branch names and heads", db.execs[1].args)
	}
}
