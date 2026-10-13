// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"strings"
	"testing"
)

// TestContentFilesLanguageCoveringIndexMigrationsSwapIndexAndVacuum pins
// the #7728 shipped migration sequence: 171 drops the old (language,
// repo_id) definition of content_files_language_repo_idx behind an
// indexed_at guard (the 128/129 shape for #6475, since shipped migration 004
// creates the name and would recreate the old definition on replay if the
// name were freed), 172 rebuilds the SAME NAME carrying indexed_at (the
// COUNT(*)/MAX(indexed_at) rollup's second column), and 173 vacuums so the
// visibility map lets the covering definition serve index-only scans
// immediately.
func TestContentFilesLanguageCoveringIndexMigrationsSwapIndexAndVacuum(t *testing.T) {
	t.Parallel()

	byName := map[string]string{}
	var order []string
	for _, def := range BootstrapDefinitions() {
		byName[def.Name] = def.SQL
		order = append(order, def.Name)
	}
	drop, okDrop := byName["drop_content_files_language_repo_idx"]
	covering, okCovering := byName["content_files_language_covering_idx"]
	vacuum, okVacuum := byName["content_files_language_covering_idx_vacuum"]
	if !okDrop || !okCovering || !okVacuum {
		t.Fatal("migrations 171/172/173 missing from BootstrapDefinitions()")
	}
	// The canonical covering line must stay byte-identical with the fresh-
	// install DDL in schema_content_store.go so migrated and new databases
	// converge on the same index.
	const coveringLine = "ON content_files (language, repo_id) INCLUDE (indexed_at);"
	if !strings.Contains(covering, coveringLine) {
		t.Fatalf("172 missing covering line %q", coveringLine)
	}
	// 171 drops only behind the guard: an unguarded drop would rebuild the
	// index on every untracked replay (schema_index_replay_test.go pins the
	// same shape through replayGuardedRedefinitions).
	if !strings.Contains(drop, "DO $$") || !strings.Contains(drop, "indexdef NOT LIKE '%indexed_at%'") {
		t.Fatal("171 must drop the old language definition inside a DO block behind the indexed_at guard")
	}
	if !strings.Contains(drop, "DROP INDEX content_files_language_repo_idx;") {
		t.Fatal("171 must drop exactly the legacy language index name")
	}
	if !strings.Contains(covering, "CREATE INDEX CONCURRENTLY IF NOT EXISTS content_files_language_repo_idx") {
		t.Fatal("172 must rebuild the same language index name concurrently")
	}
	if !strings.Contains(vacuum, "VACUUM (ANALYZE) content_files;") {
		t.Fatal("173 must vacuum content_files")
	}
	// Sole-statement pin for 172 and 173: a second statement in either file
	// would put the whole file in one implicit transaction and fail the
	// concurrent build or the vacuum. 171 is a guarded DO block (one
	// statement, many semicolons), so it is pinned by shape above instead.
	// Comment prose is stripped first so header punctuation cannot trip it.
	for name, sql := range map[string]string{
		"172": covering,
		"173": vacuum,
	} {
		var code strings.Builder
		for line := range strings.Lines(sql) {
			if i := strings.Index(line, "--"); i >= 0 {
				line = line[:i]
			}
			code.WriteString(line)
		}
		if strings.Count(code.String(), ";") != 1 {
			t.Fatalf("migration %s must hold a single statement", name)
		}
	}
	pos := map[string]int{}
	for i, name := range order {
		pos[name] = i
	}
	if pos["drop_content_files_language_repo_idx"] >= pos["content_files_language_covering_idx"] ||
		pos["content_files_language_covering_idx"] >= pos["content_files_language_covering_idx_vacuum"] {
		t.Fatal("migrations must apply in drop, rebuild, vacuum order")
	}
}
