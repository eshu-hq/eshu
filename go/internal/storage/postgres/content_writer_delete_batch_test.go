// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// TestContentWriterBatchesTombstoneDeletes proves that tombstoned file and
// entity records are deleted via batched DELETE ... WHERE (repo_id, col)
// IN (...) queries instead of the old per-row round trips. For 100
// deleted files + 5 deleted entities, the old code issued 305 DELETE
// ExecContext calls; the batched path should issue ~4 (one per table,
// chunked at contentFileBatchSize=500).
func TestContentWriterBatchesTombstoneDeletes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	records := make([]content.Record, 100)
	for i := 0; i < 100; i++ {
		records[i] = content.Record{
			Path:    fmt.Sprintf("deleted_file_%d.go", i),
			Deleted: true,
		}
	}
	entities := make([]content.EntityRecord, 5)
	for i := 0; i < 5; i++ {
		entities[i] = content.EntityRecord{
			EntityID:   fmt.Sprintf("deleted_entity_%d", i),
			Path:       fmt.Sprintf("file_%d.go", i),
			EntityType: "function",
			EntityName: fmt.Sprintf("func_%d", i),
			StartLine:  1,
			Deleted:    true,
		}
	}

	mat := content.Materialization{
		RepoID:       "test-repo",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Records:      records,
		Entities:     entities,
	}

	result, err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got, want := result.DeletedCount, 105; got != want {
		t.Fatalf("DeletedCount = %d, want %d", got, want)
	}

	// Count DELETE exec calls — the batched path must bound them to ~4, not 300+.
	deleteExecCount := 0
	var entityPathBatchQuery, entityIDBatchQuery, refBatchQuery, fileBatchQuery string
	for _, e := range db.execs {
		if !strings.Contains(strings.ToUpper(e.query), "DELETE") {
			continue
		}
		deleteExecCount++
		switch {
		case strings.Contains(e.query, "DELETE FROM content_entities") && strings.Contains(e.query, "relative_path"):
			entityPathBatchQuery = e.query
		case strings.Contains(e.query, "DELETE FROM content_entities") && strings.Contains(e.query, "entity_id"):
			entityIDBatchQuery = e.query
		case strings.Contains(e.query, "DELETE FROM content_file_references"):
			refBatchQuery = e.query
		case strings.Contains(e.query, "DELETE FROM content_files"):
			fileBatchQuery = e.query
		}
	}

	if deleteExecCount > 10 {
		t.Fatalf("DELETE exec count = %d, want ≤ ~4 batched deletes (old per-row would be 305)", deleteExecCount)
	}

	// Each batch query must use IN (...) syntax.
	for name, q := range map[string]string{
		"entity path batch": entityPathBatchQuery,
		"entity ID batch":   entityIDBatchQuery,
		"reference batch":   refBatchQuery,
		"file batch":        fileBatchQuery,
	} {
		if q == "" {
			t.Fatalf("missing %s DELETE query", name)
		}
		if !strings.Contains(q, "IN (") {
			t.Fatalf("%s query should use IN (...) syntax: %s", name, q)
		}
	}

	// Output equivalence: the batched query must target the same keys the
	// per-row path would have — all 100 (repo_id, path) pairs for
	// entity/reference/file deletes, and all 5 (repo_id, entity_id) pairs
	// for entity-id deletes.
	if got, want := strings.Count(entityPathBatchQuery, "($"), 100; got != want {
		t.Fatalf("entity path batch row count = %d, want %d", got, want)
	}
	if got, want := strings.Count(refBatchQuery, "($"), 100; got != want {
		t.Fatalf("reference batch row count = %d, want %d", got, want)
	}
	if got, want := strings.Count(fileBatchQuery, "($"), 100; got != want {
		t.Fatalf("file batch row count = %d, want %d", got, want)
	}
	if got, want := strings.Count(entityIDBatchQuery, "($"), 5; got != want {
		t.Fatalf("entity ID batch row count = %d, want %d", got, want)
	}
}
