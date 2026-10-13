// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Probe 7728 step 1 (RED): the by-language read path must be served by a
// covering language index. This test fails until the language/repository
// index carries indexed_at, which is what lets Postgres answer the
// COUNT(*)/MAX(indexed_at) rollup without visiting the content_files heap.
package postgres

import (
	"strings"
	"testing"
)

func TestContentFilesLanguageRepoIndexCoversIndexedAt(t *testing.T) {
	t.Parallel()

	if !strings.Contains(contentStoreFilterIndexSchemaSQL, "content_files_language_repo_idx") {
		t.Fatal("filter index SQL missing content_files_language_repo_idx")
	}
	// The canonical covering line must stay byte-identical with migration
	// 172 so migrated and fresh-install databases converge on the same
	// index. It lets the by-language COUNT(*)/MAX(indexed_at) GROUP BY in
	// go/internal/query/content_reader_language_inventory.go run as an
	// index-only scan instead of one heap fetch per file row (#7728).
	const coveringLine = "ON content_files (language, repo_id) INCLUDE (indexed_at);"
	if !strings.Contains(contentStoreFilterIndexSchemaSQL, coveringLine) {
		t.Fatalf("filter index SQL missing covering line %q", coveringLine)
	}
}
