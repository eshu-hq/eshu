// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// singleRowMetadataSetPattern extracts the UPDATE ... SET list.
var singleRowMetadataSetPattern = regexp.MustCompile(`(?is)^\s*UPDATE\s+content_files\s+AS\s+c\s+SET\s+(.*?)\s+WHERE\b`)

// singleRowMetadataRecheckFragments is the recheck twin the single-row
// metadata-only UPDATE must carry: every guard column compared in SQL, plus
// the lineage-differ predicate. It mirrors the batch UPDATE's map; both
// statements are pinned to the same guard, so a new guarded column fails both
// tests until both statements recheck it.
var singleRowMetadataRecheckFragments = map[string]string{
	"commit_sha":       "c.commit_sha IS DISTINCT FROM $3",
	"content_hash":     "c.content_hash = $5",
	"line_count":       "c.line_count IS NOT DISTINCT FROM $8",
	"language":         "c.language IS NOT DISTINCT FROM $9",
	"artifact_type":    "c.artifact_type IS NOT DISTINCT FROM $10",
	"template_dialect": "c.template_dialect IS NOT DISTINCT FROM $11",
	"iac_relevant":     "c.iac_relevant IS NOT DISTINCT FROM $12",
	"generation_id":    "c.generation_id IS DISTINCT FROM $4",
	"content":          "c.content IS NOT DISTINCT FROM $7",
}

// TestSingleRowMetadataUpdateSETAssignsOnlyLineage pins the HOT contract for
// the single-row path: the metadata UPDATE assigns exactly commit_sha and
// generation_id from the incoming lineage. Assigning content (or its generated
// content_lower copy) or indexed_at would cost the HOT update.
func TestSingleRowMetadataUpdateSETAssignsOnlyLineage(t *testing.T) {
	t.Parallel()

	set := singleRowMetadataSetPattern.FindStringSubmatch(updateSingleMetadataOnlyContentFileQuery)
	if set == nil {
		t.Fatal("SET list not found in updateSingleMetadataOnlyContentFileQuery")
	}
	got := map[string]string{}
	for _, item := range splitColumns(set[1]) {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("SET item %q is not an assignment", item)
		}
		got[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	want := map[string]string{"commit_sha": "$3", "generation_id": "$4"}
	if !mapsEqual(got, want) {
		t.Fatalf("SET assigns %v, want %v", got, want)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestSingleRowMetadataUpdateRechecksEveryGuardColumn keeps the single-row
// UPDATE in step with the guarded upsert: it must recheck every equality the
// guard compares (so a concurrent change between the UPDATE and the upsert
// falls through correctly), plus the lineage-differ predicate. The only guard
// column it must not carry is indexed_at, which it never moves.
func TestSingleRowMetadataUpdateRechecksEveryGuardColumn(t *testing.T) {
	t.Parallel()

	guard := guardTuplesPattern.FindStringSubmatch(upsertContentFileConflictClause)
	if guard == nil {
		t.Fatal("guard tuples not found in upsertContentFileConflictClause")
	}
	var guardColumns []string
	for _, item := range splitColumns(guard[1]) {
		guardColumns = append(guardColumns, strings.TrimPrefix(item, "content_files."))
	}
	guardColumns = append(guardColumns, contentFileSeparateCompare)

	for _, column := range guardColumns {
		fragment, ok := singleRowMetadataRecheckFragments[column]
		if !ok {
			t.Errorf("guard column %q has no recheck in the single-row metadata UPDATE; teach the UPDATE about it", column)
			continue
		}
		if !strings.Contains(updateSingleMetadataOnlyContentFileQuery, fragment) {
			t.Errorf("single-row metadata UPDATE lacks %q for guard column %q", fragment, column)
		}
	}
	for column := range singleRowMetadataRecheckFragments {
		if !slices.Contains(guardColumns, column) {
			t.Errorf("recheck column %q is not a guard column of the shipped clause", column)
		}
	}
	if !strings.Contains(updateSingleMetadataOnlyContentFileQuery, "octet_length(c.content) = $6") {
		t.Errorf("single-row metadata UPDATE lacks the body-size recheck")
	}
	if strings.Contains(updateSingleMetadataOnlyContentFileQuery, "indexed_at") {
		t.Errorf("single-row metadata UPDATE must not mention indexed_at: moving it costs the HOT update")
	}
	if !strings.Contains(updateSingleMetadataOnlyContentFileQuery, "RETURNING c.repo_id, c.relative_path") {
		t.Errorf("single-row metadata UPDATE lacks the RETURNING keys the fall-through needs")
	}
}

// TestContentStoreUpsertFileBatchTriesMetadataUpdateFirst pins the #8950
// single-row rollout: a row the metadata-only UPDATE touched skips the guarded
// upsert entirely.
func TestContentStoreUpsertFileBatchTriesMetadataUpdateFirst(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"repo-1", "main.go"}}},
		},
	}
	store := NewContentStore(db)

	files := []content.Record{
		{Path: "main.go", Body: "package main\n", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
	}
	if err := store.UpsertFileBatch(context.Background(), "repo-1", "generation-1", files); err != nil {
		t.Fatalf("UpsertFileBatch() error = %v, want nil", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1 (the metadata-only UPDATE)", len(db.queries))
	}
	if q := db.queries[0].query; !strings.Contains(q, "UPDATE content_files") {
		t.Fatalf("query = %q, want the metadata-only UPDATE first", q)
	}
	if got := len(db.queries[0].args); got != 12 {
		t.Fatalf("metadata UPDATE arg count = %d, want 12", got)
	}
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO content_files") {
			t.Fatalf("guarded upsert issued for a metadata-updated row: %q", exec.query)
		}
	}
}

// TestContentStoreUpsertFileBatchFallsThroughWhenMetadataUpdateMisses pins the
// backstop: a row the metadata-only UPDATE did not touch (insert, content
// change, or identical row) still goes through the guarded upsert.
func TestContentStoreUpsertFileBatchFallsThroughWhenMetadataUpdateMisses(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewContentStore(db)

	files := []content.Record{
		{Path: "main.go", Body: "package main\n", Metadata: map[string]string{"commit_sha": "c2", "language": "go"}},
	}
	if err := store.UpsertFileBatch(context.Background(), "repo-1", "generation-1", files); err != nil {
		t.Fatalf("UpsertFileBatch() error = %v, want nil", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1 (the metadata-only UPDATE attempt)", len(db.queries))
	}
	if q := db.queries[0].query; !strings.Contains(q, "UPDATE content_files") {
		t.Fatalf("query = %q, want the metadata-only UPDATE attempt first", q)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1 (the guarded upsert fall-through)", len(db.execs))
	}
	if q := db.execs[0].query; !strings.Contains(q, "INSERT INTO content_files") {
		t.Fatalf("exec = %q, want the guarded upsert fall-through", q)
	}
}
