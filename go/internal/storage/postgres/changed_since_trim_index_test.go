// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestBootstrapDefinitionsIncludeChangedSinceTrimIndexes pins the #7098
// slice D-read trim: two partial indexes that keep the changed-since diff
// from fetching reducer-derived TOAST payloads only to discard them, and the
// tombstone CTE from range-scanning whole generations.
//
// The predicates must stay byte-identical to the diff statement's filters so
// a generic plan (the mode services get through database/sql) can prove the
// index applies: the LIKE arm is asserted against
// changedSinceExcludeReducerDerivedKinds itself, not a copy, so the two
// cannot drift. The read statement is unchanged by this slice.
func TestBootstrapDefinitionsIncludeChangedSinceTrimIndexes(t *testing.T) {
	t.Parallel()

	for _, want := range []struct {
		name          string
		path          string
		index         string
		tombstoneSide string
	}{
		{
			name:          "fact_records_changed_since_live_keys_idx",
			path:          "go/internal/storage/postgres/migrations/194_fact_records_changed_since_live_keys_idx.sql",
			index:         "fact_records_changed_since_live_keys_idx",
			tombstoneSide: "is_tombstone = FALSE",
		},
		{
			name:          "fact_records_changed_since_tombstone_keys_idx",
			path:          "go/internal/storage/postgres/migrations/195_fact_records_changed_since_tombstone_keys_idx.sql",
			index:         "fact_records_changed_since_tombstone_keys_idx",
			tombstoneSide: "is_tombstone = TRUE",
		},
	} {
		var def Definition
		for _, d := range BootstrapDefinitions() {
			if d.Name == want.name {
				def = d
				break
			}
		}
		if def.Name == "" {
			t.Fatalf("changed-since trim definition %q missing", want.name)
		}
		if def.Path != want.path {
			t.Fatalf("definition %q Path = %q, want %q", want.name, def.Path, want.path)
		}
		for _, fragment := range []string{
			"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + want.index,
			"ON fact_records (scope_id, generation_id)",
			want.tombstoneSide,
			changedSinceExcludeReducerDerivedKinds,
		} {
			if !strings.Contains(def.SQL, fragment) {
				t.Fatalf("definition %q SQL missing %q:\n%s", want.name, fragment, def.SQL)
			}
		}
		// CONCURRENTLY cannot run inside the implicit transaction a
		// multi-statement file forms: the lone statement in the file.
		// Comment lines hold prose semicolons, so strip them first.
		var code strings.Builder
		for line := range strings.Lines(def.SQL) {
			if trimmed := strings.TrimSpace(line); !strings.HasPrefix(trimmed, "--") {
				code.WriteString(line)
			}
		}
		if got := strings.Count(code.String(), ";"); got != 1 {
			t.Fatalf("definition %q has %d statement terminators, want 1", want.name, got)
		}
	}
}
