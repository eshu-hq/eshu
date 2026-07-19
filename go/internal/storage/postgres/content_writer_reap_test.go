// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/lib/pq"
)

// TestContentWriterReapsStaleEntityOnIDChurn is the id-churn regression this
// reap exists for: package.json's line_number fix (#5329) makes a
// dependency's content.CanonicalEntityID change whenever its source line
// moves, even though it is the same logical dependency. Before this reap,
// Write() only ever deleted a content_entities row via entity.Deleted (an
// explicit tombstone) or a path-scoped purge — never because a row was
// simply superseded by a fresh identity for the same file. A rewrite of
// package.json that shifts "react" from line 12 to line 13 produces a new
// entity_id for react while lodash (unchanged line) keeps its old entity_id.
//
// This test proves the reap DELETE anti-joins against the COMPLETE fresh
// per-path entity_id set: react's new id and lodash's unchanged id are both
// in the keep-set, so a stale row for react's OLD id (not present in this
// Write() call at all) would be the only thing the DELETE removes.
func TestContentWriterReapsStaleEntityOnIDChurn(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Simulates the fresh entity set for package.json after a line-shifting
	// edit: react's entity_id changed (new line), lodash's did not.
	entities := []content.EntityRecord{
		{
			EntityID:   "github.com/acme/app:package.json:Variable:react:13",
			Path:       "package.json",
			EntityType: "Variable",
			EntityName: "react",
			StartLine:  13,
		},
		{
			EntityID:   "github.com/acme/app:package.json:Variable:lodash:20",
			Path:       "package.json",
			EntityType: "Variable",
			EntityName: "lodash",
			StartLine:  20,
		},
	}

	mat := content.Materialization{
		RepoID:       "github.com/acme/app",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Entities:     entities,
	}

	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	reapQuery, reapArgs := findReapExec(t, db)
	if !strings.Contains(reapQuery, "DELETE FROM content_entities") {
		t.Fatalf("reap query = %q, want a DELETE FROM content_entities", reapQuery)
	}
	if !strings.Contains(reapQuery, "relative_path = ANY(") || !strings.Contains(reapQuery, "entity_id <> ALL(") {
		t.Fatalf("reap query missing anti-join shape: %s", reapQuery)
	}

	if got, want := reapArgs[0].(string), "github.com/acme/app"; got != want {
		t.Fatalf("reap repo_id arg = %q, want %q", got, want)
	}
	paths, ok := reapArgs[1].(pq.StringArray)
	if !ok {
		t.Fatalf("reap path arg type = %T, want pq.StringArray", reapArgs[1])
	}
	if len(paths) != 1 || paths[0] != "package.json" {
		t.Fatalf("reap paths = %v, want [package.json]", []string(paths))
	}
	freshIDs, ok := reapArgs[2].(pq.StringArray)
	if !ok {
		t.Fatalf("reap fresh-id arg type = %T, want pq.StringArray", reapArgs[2])
	}

	// Both the churned (react, new line) and unchanged (lodash) fresh ids
	// must be in the keep-set, so neither is accidentally reaped.
	mustContain(t, freshIDs, "github.com/acme/app:package.json:Variable:react:13")
	mustContain(t, freshIDs, "github.com/acme/app:package.json:Variable:lodash:20")

	// react's OLD (pre-edit) entity_id is NOT in the fresh set — proving that
	// if a stale row for it existed in content_entities, this anti-join
	// would correctly delete it (entity_id <> ALL(freshIDs) is true for it).
	mustNotContain(t, freshIDs, "github.com/acme/app:package.json:Variable:react:12")
}

// TestContentWriterReapsRemovedEntityAlongsideSurvivor is the "removed
// dependency" variant of the same defect class: a package.json edit that
// drops one dependency entirely (no line-number churn involved) while
// another dependency in the same file is untouched. Before this reap,
// nothing retracted the removed dependency's row — the reducer only ever
// saw entity.Deleted for a whole-file delete, never for a single entity
// dropped out of a file that otherwise still exists. This is a real bug on
// main today independent of the line_number fix: it fails without the reap
// regardless of whether entity identity is line-number-based or not.
func TestContentWriterReapsRemovedEntityAlongsideSurvivor(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Fresh set after the edit: only "kept-dep" (B) remains; "removed-dep"
	// (A) is gone from the source and therefore absent from this Write()
	// call — there is no entity.Deleted for it, matching how the reducer
	// actually observes a dependency that silently disappeared.
	entities := []content.EntityRecord{
		{
			EntityID:   "repo-1:composer.json:Variable:kept-dep:5",
			Path:       "composer.json",
			EntityType: "Variable",
			EntityName: "kept-dep",
			StartLine:  5,
		},
	}

	mat := content.Materialization{
		RepoID:       "repo-1",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Entities:     entities,
	}

	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	_, reapArgs := findReapExec(t, db)
	freshIDs, ok := reapArgs[2].(pq.StringArray)
	if !ok {
		t.Fatalf("reap fresh-id arg type = %T, want pq.StringArray", reapArgs[2])
	}

	mustContain(t, freshIDs, "repo-1:composer.json:Variable:kept-dep:5")
	// removed-dep's id is not present in this Write() call's fresh set, so
	// the anti-join reaps it — proving a dependency that disappears from a
	// still-present file no longer persists forever.
	mustNotContain(t, freshIDs, "repo-1:composer.json:Variable:removed-dep:9")
}

// TestContentWriterReapDoesNotLabelFilter is the mixed-label guard: the
// anti-over-delete regression that matters most. A file with both a
// Function entity and a Variable entity is reprocessed after only the
// Variable changed (the Function's line, and therefore its entity_id, is
// untouched). The reap MUST be scoped to the file's COMPLETE fresh entity
// set across every label, not filtered to only the label that happened to
// change — otherwise the Function row would be excluded from the anti-join
// keep-set and this reap would delete it, an over-delete in the #5147/#5327
// defect class the reap is explicitly designed to avoid (see
// reapStaleContentEntities's doc comment on content_writer_reap.go).
//
// This test breaks if a future change filters freshIDsByPath by EntityType
// instead of grouping the whole path's fresh rows: the Function id would
// then be missing from the keep-set and mustContain would fail.
func TestContentWriterReapDoesNotLabelFilter(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	entities := []content.EntityRecord{
		{
			// Unchanged Function — reprocessed with the SAME entity_id
			// because its line did not move.
			EntityID:   "repo-1:app.js:Function:handler:3",
			Path:       "app.js",
			EntityType: "Function",
			EntityName: "handler",
			StartLine:  3,
		},
		{
			// Changed Variable — new entity_id because its line moved.
			EntityID:   "repo-1:app.js:Variable:config:41",
			Path:       "app.js",
			EntityType: "Variable",
			EntityName: "config",
			StartLine:  41,
		},
	}

	mat := content.Materialization{
		RepoID:       "repo-1",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Entities:     entities,
	}

	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	_, reapArgs := findReapExec(t, db)
	freshIDs, ok := reapArgs[2].(pq.StringArray)
	if !ok {
		t.Fatalf("reap fresh-id arg type = %T, want pq.StringArray", reapArgs[2])
	}

	// The Function row's fresh id MUST be present in the keep-set even
	// though only the Variable in this file changed — a label-filtered
	// reap would omit it here and delete a still-valid, unrelated-label row.
	mustContain(t, freshIDs, "repo-1:app.js:Function:handler:3")
	mustContain(t, freshIDs, "repo-1:app.js:Variable:config:41")
}

// findReapExec locates the single stale-entity reap DELETE among db.execs
// and returns its query text and bound args, failing the test if it is
// missing or duplicated.
func findReapExec(t *testing.T, db *fakeExecQueryer) (string, []any) {
	t.Helper()

	var query string
	var args []any
	matches := 0
	for _, e := range db.execs {
		if strings.Contains(e.query, "DELETE FROM content_entities") && strings.Contains(e.query, "entity_id <> ALL") {
			query = e.query
			args = e.args
			matches++
		}
	}
	if matches == 0 {
		t.Fatalf("no stale-entity reap DELETE found among %d execs", len(db.execs))
	}
	if matches > 1 {
		t.Fatalf("expected exactly one reap DELETE, found %d", matches)
	}
	return query, args
}

func mustContain(t *testing.T, ids pq.StringArray, want string) {
	t.Helper()
	for _, id := range ids {
		if id == want {
			return
		}
	}
	t.Fatalf("fresh entity_id set %v does not contain %q", []string(ids), want)
}

func mustNotContain(t *testing.T, ids pq.StringArray, unwanted string) {
	t.Helper()
	for _, id := range ids {
		if id == unwanted {
			t.Fatalf("fresh entity_id set %v unexpectedly contains %q", []string(ids), unwanted)
		}
	}
}
