// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestUpsertContentFileBatchDuplicateKeyIsRepoAndPath pins the duplicate
// check to the conflict target, (repo_id, relative_path): the same relative
// path under two repositories is two rows and must be accepted, while the same
// repository and path twice is the one thing the batch rejects, before any
// statement runs (#8915).
func TestUpsertContentFileBatchDuplicateKeyIsRepoAndPath(t *testing.T) {
	t.Parallel()
	row := func(repoID, path string) preparedFileRow {
		return preparedFileRow{repoID: repoID, path: path, body: "package p\n", contentHash: "h", lineCount: 1}
	}
	indexedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("same path under different repositories is accepted", func(t *testing.T) {
		t.Parallel()
		db := &fakeExecQueryer{}
		writer := NewContentWriter(withTransactions(db))
		batch := []preparedFileRow{row("repo-a", "src/main.go"), row("repo-b", "src/main.go")}
		if err := writer.upsertContentFileBatch(context.Background(), batch, indexedAt, "gen-1"); err != nil {
			t.Fatalf("upsertContentFileBatch() error = %v, want nil: the conflict target includes repo_id", err)
		}
		inserted := 0
		for _, exec := range db.execs {
			if strings.Contains(exec.query, "INSERT INTO content_files") {
				inserted++
			}
		}
		if inserted != 1 {
			t.Fatalf("content_files INSERT statements = %d, want 1 multi-row statement", inserted)
		}
	})

	t.Run("same repository and path twice is rejected before any statement", func(t *testing.T) {
		t.Parallel()
		db := &fakeExecQueryer{}
		writer := NewContentWriter(withTransactions(db))
		batch := []preparedFileRow{row("repo-a", "src/main.go"), row("repo-b", "src/main.go"), row("repo-a", "src/main.go")}
		err := writer.upsertContentFileBatch(context.Background(), batch, indexedAt, "gen-1")
		if err == nil || !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), "repo-a") {
			t.Fatalf("upsertContentFileBatch() error = %v, want a duplicate error naming repo-a", err)
		}
		if len(db.execs) != 0 {
			t.Fatalf("executed %d statements for a rejected batch, want none", len(db.execs))
		}
	})
}
