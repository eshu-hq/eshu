// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// deletedPathsByTable returns, per table, the (repo_id, relative_path) pairs the
// recorded (repo_id, relative_path) IN (...) deletes carried, as "repo|path".
func deletedPathsByTable(fake *fakeExecQueryer) map[string][]string {
	out := map[string][]string{}
	for _, exec := range fake.execs {
		for _, table := range []string{"content_entities", "content_file_references", "content_files"} {
			if !strings.HasPrefix(exec.query, "DELETE FROM "+table+" WHERE (repo_id, relative_path) IN (") {
				continue
			}
			for i := 0; i+1 < len(exec.args); i += 2 {
				out[table] = append(out[table], exec.args[i].(string)+"|"+exec.args[i+1].(string))
			}
		}
	}
	for table := range out {
		sort.Strings(out[table])
	}
	return out
}

// requireSubset fails unless every want pair is in got.
func requireSubset(t *testing.T, label string, want, got []string) {
	t.Helper()
	have := make(map[string]struct{}, len(got))
	for _, pair := range got {
		have[pair] = struct{}{}
	}
	for _, pair := range want {
		if _, ok := have[pair]; !ok {
			t.Fatalf("%s = %v, missing %s", label, got, pair)
		}
	}
}

// requireOnlyFreshDeletes fails when any entity or file row is deleted, or when
// a reference delete names a path outside fresh. The reference refresh of a
// touched file is expected and is the only delete a write with nothing stale
// may issue.
func requireOnlyFreshDeletes(t *testing.T, deletes map[string][]string, fresh ...string) {
	t.Helper()
	for _, table := range []string{"content_entities", "content_files"} {
		if len(deletes[table]) != 0 {
			t.Fatalf("%s deletes = %v, want none", table, deletes[table])
		}
	}
	allowed := make(map[string]struct{}, len(fresh))
	for _, pair := range fresh {
		allowed[pair] = struct{}{}
	}
	for _, pair := range deletes["content_file_references"] {
		if _, ok := allowed[pair]; !ok {
			t.Fatalf("content_file_references delete names %s, which is not a fresh path %v", pair, fresh)
		}
	}
}

func listedContentFilePathReads(fake *fakeExecQueryer) int {
	reads := 0
	for _, query := range fake.queries {
		if query.query == listContentFilePathsSQL {
			reads++
		}
	}
	return reads
}

func storedPathRows(paths ...string) [][]any {
	rows := make([][]any, 0, len(paths))
	for _, path := range paths {
		rows = append(rows, []any{path})
	}
	return rows
}

// TestContentWriterFullSnapshotDeletesStoredPathsItLacks pins the writer-side
// contract of #7447 item 5 without a database: a full snapshot reads the
// repository's stored paths once, and exactly the stored paths its Records do
// not carry go through the entity, reference and file deletes. The live tests
// prove the same against PostgreSQL; this one runs in the default pass.
func TestContentWriterFullSnapshotDeletesStoredPathsItLacks(t *testing.T) {
	t.Parallel()

	fake := &fakeExecQueryer{contentFilePathRows: storedPathRows("keep.go", "x.go", "old/b.go", "y.go")}
	writer := NewContentWriter(withTransactions(fake))

	_, err := writer.Write(context.Background(), content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-d", FullSnapshot: true,
		Records: []content.Record{
			{Path: "keep.go", Body: "package p\n"},
			{Path: "y.go", Body: "package p\n"},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if got := listedContentFilePathReads(fake); got != 1 {
		t.Fatalf("stored-path reads = %d, want 1", got)
	}
	want := []string{"repo-1|old/b.go", "repo-1|x.go"}
	deletes := deletedPathsByTable(fake)
	// Only the reap deletes entity and file rows for a path the Records carry
	// no tombstone for, so those two tables must match the stale set exactly.
	for _, table := range []string{"content_entities", "content_files"} {
		if !reflect.DeepEqual(deletes[table], want) {
			t.Fatalf("%s deleted pairs = %v, want %v", table, deletes[table], want)
		}
	}
	// Every touched file also has its references deleted before they are
	// re-inserted, with the same statement text, so that table must contain the
	// stale set and may also name the fresh paths.
	requireSubset(t, "content_file_references deleted pairs", want, deletes["content_file_references"])
}

// TestContentWriterDeltaMaterializationNeverReadsStoredPaths pins the zero
// value: without FullSnapshot no stored-path read is issued and nothing is
// deleted for a path the Records do not name.
func TestContentWriterDeltaMaterializationNeverReadsStoredPaths(t *testing.T) {
	t.Parallel()

	fake := &fakeExecQueryer{contentFilePathRows: storedPathRows("keep.go", "x.go")}
	writer := NewContentWriter(withTransactions(fake))

	_, err := writer.Write(context.Background(), content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-b",
		Records: []content.Record{{Path: "keep.go", Body: "package p\n"}},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if got := listedContentFilePathReads(fake); got != 0 {
		t.Fatalf("stored-path reads = %d, want 0 for a materialization that is not a full snapshot", got)
	}
	requireOnlyFreshDeletes(t, deletedPathsByTable(fake), "repo-1|keep.go")
}

// TestContentWriterFullSnapshotWithNothingStaleDeletesNothing pins the common
// case, every first generation and most reconciliation snapshots: one read and
// no delete.
func TestContentWriterFullSnapshotWithNothingStaleDeletesNothing(t *testing.T) {
	t.Parallel()

	fake := &fakeExecQueryer{contentFilePathRows: storedPathRows("keep.go")}
	writer := NewContentWriter(withTransactions(fake))

	_, err := writer.Write(context.Background(), content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-d", FullSnapshot: true,
		Records: []content.Record{{Path: "keep.go", Body: "package p\n"}},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if got := listedContentFilePathReads(fake); got != 1 {
		t.Fatalf("stored-path reads = %d, want 1", got)
	}
	requireOnlyFreshDeletes(t, deletedPathsByTable(fake), "repo-1|keep.go")
}

// TestContentWriterFullSnapshotDerivesInfraInventoryForReapedPaths pins that a
// reaped path also leaves the infra read model. The derive mirrors exactly the
// paths of the Records it is given, so a stale path that was deleted from
// content_entities but never handed to it would keep its
// infra_resource_entities rows, and the read model would stop mirroring the
// content store with nothing marking the repository dirty.
func TestContentWriterFullSnapshotDerivesInfraInventoryForReapedPaths(t *testing.T) {
	t.Parallel()

	inner := &fakeExecQueryer{contentFilePathRows: storedPathRows("a.tf", "b.tf")}
	database := withTransactions(inner)
	writer := NewContentWriter(database)

	_, err := writer.Write(context.Background(), content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-d", FullSnapshot: true,
		Records: []content.Record{{Path: "a.tf", Body: "resource {}"}},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var derived []string
	for _, call := range database.txExecs {
		if strings.Contains(call.query, "DELETE FROM infra_resource_entities") {
			derived = append(derived, []string(call.args[1].(array.StringArray))...)
		}
	}
	sort.Strings(derived)
	if want := []string{"a.tf", "b.tf"}; !reflect.DeepEqual(derived, want) {
		t.Fatalf("infra derive paths = %v, want %v: the reaped path b.tf must be re-derived", derived, want)
	}
}

// TestContentWriterFullSnapshotKeepsRetainedPaths pins the collector-skip case
// from the #7447 review. A file whose body could not be re-read has a file fact
// (the graph File node stays) but no content Record, so the projector passes its
// path as retained. The reap must leave its stored content alone and still
// remove a path that is in neither the Records nor the retained set.
func TestContentWriterFullSnapshotKeepsRetainedPaths(t *testing.T) {
	t.Parallel()

	fake := &fakeExecQueryer{contentFilePathRows: storedPathRows("kept.go", "skipped.go", "gone.go")}
	writer := NewContentWriter(withTransactions(fake))

	_, err := writer.Write(context.Background(), content.Materialization{
		RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-d", FullSnapshot: true,
		Records:       []content.Record{{Path: "kept.go", Body: "package p\n"}},
		RetainedPaths: []string{"skipped.go"},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	want := []string{"repo-1|gone.go"}
	deletes := deletedPathsByTable(fake)
	for _, table := range []string{"content_entities", "content_files"} {
		if !reflect.DeepEqual(deletes[table], want) {
			t.Fatalf("%s deleted pairs = %v, want only the path absent from the snapshot %v", table, deletes[table], want)
		}
	}
}
