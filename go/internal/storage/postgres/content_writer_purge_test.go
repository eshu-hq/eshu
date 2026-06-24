package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// TestContentWriterPurgeEntitiesDeletesEntitiesNotFile verifies that a Record
// with PurgeEntities=true causes the writer to DELETE content_entities for that
// path (retract stale symbols from a prior indexing run) while still upserting
// the content file row. The file itself must NOT be deleted.
//
// This is the correct behavior when the per-file entity cap fires: the file
// body is still valuable for BM25 search, but previously-indexed entity rows
// for the path must be purged so they are not left queryable as orphans.
func TestContentWriterPurgeEntitiesDeletesEntitiesNotFile(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	mat := content.Materialization{
		RepoID:       "test-repo",
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Records: []content.Record{
			{
				Path:          "big_file.js",
				Body:          "/* oversized */",
				PurgeEntities: true,
				Deleted:       false,
			},
		},
	}

	result, err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got, want := result.RecordCount, 1; got != want {
		t.Fatalf("RecordCount = %d, want %d", got, want)
	}
	// PurgeEntities is not a delete — the record itself survives.
	if got, want := result.DeletedCount, 0; got != want {
		t.Fatalf("DeletedCount = %d, want %d (PurgeEntities must not count as a delete)", got, want)
	}

	// Expected exec sequence:
	//   [0] DELETE FROM content_file_references  (stale reference pre-delete, always runs)
	//   [1] DELETE FROM content_entities          (PurgeEntities path-scoped entity delete)
	//   [2] INSERT INTO content_files             (file upsert)
	// Order of [0] vs [1] may vary depending on implementation; assert by content.

	var entityDeleteQuery, fileDeleteQuery, fileUpsertQuery string
	for _, e := range db.execs {
		switch {
		case strings.Contains(e.query, "DELETE FROM content_entities") && !strings.Contains(e.query, "entity_id"):
			entityDeleteQuery = e.query
		case strings.Contains(e.query, "DELETE FROM content_files"):
			fileDeleteQuery = e.query
		case strings.Contains(e.query, "INSERT INTO content_files"):
			fileUpsertQuery = e.query
		}
	}

	// Must have a path-scoped entity DELETE.
	if entityDeleteQuery == "" {
		t.Fatalf("expected DELETE FROM content_entities for big_file.js but found none; execs: %v", db.execs)
	}

	// Must have a content_files upsert.
	if fileUpsertQuery == "" {
		t.Fatalf("expected INSERT INTO content_files for big_file.js but found none; execs: %v", db.execs)
	}

	// Must NOT have a content_files delete.
	if fileDeleteQuery != "" {
		t.Fatalf("unexpected DELETE FROM content_files for PurgeEntities record (not a delete): %s", fileDeleteQuery)
	}
}
