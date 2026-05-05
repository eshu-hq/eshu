package content_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	pg "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestPostgresContentWriterUpsertsFileAndEntityRowsAndDeletesTombstones(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &recordingExecQueryer{}
	writer := pg.NewContentWriter(db)
	writer.Now = func() time.Time { return now }

	result, err := writer.Write(context.Background(), content.Materialization{
		RepoID:       "repository:r_test",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Records: []content.Record{
			{
				Path:   "schema.sql",
				Body:   "CREATE TABLE users (\n  id BIGINT\n);\n",
				Digest: "digest-1",
				Metadata: map[string]string{
					"language":         "sql",
					"artifact_type":    "schema",
					"template_dialect": "ansi",
					"iac_relevant":     "true",
					"commit_sha":       "abc123",
				},
			},
			{
				Path:    "old.sql",
				Deleted: true,
			},
		},
		Entities: []content.EntityRecord{
			{
				EntityID:        "content-entity:e_ab12cd34ef56",
				Path:            "schema.sql",
				EntityType:      "SqlTable",
				EntityName:      "public.users",
				StartLine:       10,
				EndLine:         20,
				StartByte:       intPtr(128),
				EndByte:         intPtr(256),
				Language:        "sql",
				ArtifactType:    "schema",
				TemplateDialect: "ansi",
				IACRelevant:     boolPtr(true),
				SourceCache:     "create table public.users",
				Metadata: map[string]any{
					"docstring":  "Primary table.",
					"decorators": []string{"@tracked"},
				},
			},
			{
				EntityID:   "content-entity:e_old",
				Path:       "old.sql",
				EntityType: "SqlTable",
				EntityName: "public.old_users",
				StartLine:  1,
				EndLine:    1,
				Deleted:    true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got, want := result.RecordCount, 2; got != want {
		t.Fatalf("Write().RecordCount = %d, want %d", got, want)
	}
	if got, want := result.EntityCount, 2; got != want {
		t.Fatalf("Write().EntityCount = %d, want %d", got, want)
	}
	if got, want := result.DeletedCount, 2; got != want {
		t.Fatalf("Write().DeletedCount = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 7; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	// Batched order: file deletes first, then file reference cleanup and
	// upsert batch, then entity deletes, then entity upsert batch.
	if !strings.Contains(db.execs[0].query, "DELETE FROM content_entities") {
		t.Fatalf("file tombstone entity cleanup query = %q, want content_entities delete", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "DELETE FROM content_file_references") {
		t.Fatalf("file tombstone reference cleanup query = %q, want content_file_references delete", db.execs[1].query)
	}
	if !strings.Contains(db.execs[2].query, "DELETE FROM content_files") {
		t.Fatalf("file delete query = %q, want content_files delete", db.execs[2].query)
	}
	if !strings.Contains(db.execs[3].query, "DELETE FROM content_file_references") {
		t.Fatalf("stale reference cleanup query = %q, want content_file_references delete", db.execs[3].query)
	}
	if !strings.Contains(db.execs[4].query, "INSERT INTO content_files") {
		t.Fatalf("upsert query = %q, want content_files insert", db.execs[4].query)
	}
	if !strings.Contains(db.execs[5].query, "DELETE FROM content_entities") {
		t.Fatalf("entity delete query = %q, want content_entities delete", db.execs[5].query)
	}
	if !strings.Contains(db.execs[6].query, "INSERT INTO content_entities") {
		t.Fatalf("entity query = %q, want content_entities insert", db.execs[6].query)
	}

	args := db.execs[4].args
	if got, want := args[0], "repository:r_test"; got != want {
		t.Fatalf("repo_id arg = %v, want %v", got, want)
	}
	if got, want := args[1], "schema.sql"; got != want {
		t.Fatalf("relative_path arg = %v, want %v", got, want)
	}
	if got, want := args[2], "abc123"; got != want {
		t.Fatalf("commit_sha arg = %v, want %v", got, want)
	}
	if got, want := args[3], "CREATE TABLE users (\n  id BIGINT\n);\n"; got != want {
		t.Fatalf("content arg = %q, want %q", got, want)
	}
	if got, want := args[4], "digest-1"; got != want {
		t.Fatalf("content_hash arg = %v, want %v", got, want)
	}
	if got, want := args[5], 3; got != want {
		t.Fatalf("line_count arg = %v, want %v", got, want)
	}
	if got, want := args[6], "sql"; got != want {
		t.Fatalf("language arg = %v, want %v", got, want)
	}
	if got, want := args[7], "schema"; got != want {
		t.Fatalf("artifact_type arg = %v, want %v", got, want)
	}
	if got, want := args[8], "ansi"; got != want {
		t.Fatalf("template_dialect arg = %v, want %v", got, want)
	}
	if got, want := args[9], true; got != want {
		t.Fatalf("iac_relevant arg = %v, want %v", got, want)
	}
	if got, want := args[10], now; got != want {
		t.Fatalf("indexed_at arg = %v, want %v", got, want)
	}

	entityArgs := db.execs[6].args
	if got, want := entityArgs[0], "content-entity:e_ab12cd34ef56"; got != want {
		t.Fatalf("entity_id arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[1], "repository:r_test"; got != want {
		t.Fatalf("repo_id arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[2], "schema.sql"; got != want {
		t.Fatalf("relative_path arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[3], "SqlTable"; got != want {
		t.Fatalf("entity_type arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[4], "public.users"; got != want {
		t.Fatalf("entity_name arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[5], 10; got != want {
		t.Fatalf("start_line arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[6], 20; got != want {
		t.Fatalf("end_line arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[7], 128; got != want {
		t.Fatalf("start_byte arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[8], 256; got != want {
		t.Fatalf("end_byte arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[9], "sql"; got != want {
		t.Fatalf("language arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[10], "schema"; got != want {
		t.Fatalf("artifact_type arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[11], "ansi"; got != want {
		t.Fatalf("template_dialect arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[12], true; got != want {
		t.Fatalf("iac_relevant arg = %v, want %v", got, want)
	}
	if got, want := entityArgs[13], "create table public.users"; got != want {
		t.Fatalf("source_cache arg = %v, want %v", got, want)
	}
	entityMetadata, ok := entityArgs[14].([]byte)
	if !ok {
		t.Fatalf("entity metadata arg = %T, want []byte", entityArgs[14])
	}
	if got, want := string(entityMetadata), `{"decorators":["@tracked"],"docstring":"Primary table."}`; got != want {
		t.Fatalf("entity metadata arg = %s, want %s", got, want)
	}
	if got, want := entityArgs[15], now; got != want {
		t.Fatalf("indexed_at arg = %v, want %v", got, want)
	}

	if got, want := db.execs[0].args[0], "repository:r_test"; got != want {
		t.Fatalf("delete repo_id arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[1], "old.sql"; got != want {
		t.Fatalf("delete relative_path arg = %v, want %v", got, want)
	}
	if got, want := db.execs[1].args[0], "repository:r_test"; got != want {
		t.Fatalf("delete repo_id arg = %v, want %v", got, want)
	}
	if got, want := db.execs[1].args[1], "old.sql"; got != want {
		t.Fatalf("delete relative_path arg = %v, want %v", got, want)
	}
	if got, want := db.execs[2].args[0], "repository:r_test"; got != want {
		t.Fatalf("delete repo_id arg = %v, want %v", got, want)
	}
	if got, want := db.execs[2].args[1], "old.sql"; got != want {
		t.Fatalf("delete relative_path arg = %v, want %v", got, want)
	}
	if got, want := db.execs[5].args[0], "repository:r_test"; got != want {
		t.Fatalf("entity delete repo_id arg = %v, want %v", got, want)
	}
	if got, want := db.execs[5].args[1], "content-entity:e_old"; got != want {
		t.Fatalf("entity delete entity_id arg = %v, want %v", got, want)
	}
}

func TestPostgresContentWriterRejectsMissingRepoID(t *testing.T) {
	t.Parallel()

	writer := pg.NewContentWriter(&recordingExecQueryer{})
	_, err := writer.Write(context.Background(), content.Materialization{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Records: []content.Record{{
			Path: "schema.sql",
			Body: "SELECT 1;\n",
		}},
	})
	if err == nil {
		t.Fatal("Write() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "repo_id") {
		t.Fatalf("Write() error = %q, want repo_id context", err)
	}
}

type recordingExecQueryer struct {
	execs []recordingExecCall
}

type recordingExecCall struct {
	query string
	args  []any
}

func (f *recordingExecQueryer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, recordingExecCall{query: query, args: args})
	return recordingResult{}, nil
}

func (f *recordingExecQueryer) QueryContext(
	_ context.Context,
	_ string,
	_ ...any,
) (pg.Rows, error) {
	return nil, context.Canceled
}

type recordingResult struct{}

func (recordingResult) LastInsertId() (int64, error) { return 0, nil }

func (recordingResult) RowsAffected() (int64, error) { return 0, nil }

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
