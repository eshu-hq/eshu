package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// --- GetFileContent tests ---

func TestContentStoreGetFileContentReturnsRow(t *testing.T) {
	t.Parallel()

	iacTrue := true
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{contentFileRow("repo-1", "main.go", "abc123", "package main", "hash1", 1, "go", "source", "", &iacTrue)}},
		},
	}
	store := NewContentStore(db)

	row, err := store.GetFileContent(context.Background(), "repo-1", "main.go")
	if err != nil {
		t.Fatalf("GetFileContent() error = %v, want nil", err)
	}
	if row == nil {
		t.Fatal("GetFileContent() = nil, want non-nil")
	}
	if got, want := row.RepoID, "repo-1"; got != want {
		t.Fatalf("RepoID = %q, want %q", got, want)
	}
	if got, want := row.RelativePath, "main.go"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if got, want := row.Content, "package main"; got != want {
		t.Fatalf("Content = %q, want %q", got, want)
	}
	if got, want := row.Language, "go"; got != want {
		t.Fatalf("Language = %q, want %q", got, want)
	}
	if row.IACRelevant == nil || !*row.IACRelevant {
		t.Fatalf("IACRelevant = %v, want true", row.IACRelevant)
	}
}

func TestContentStoreGetFileContentReturnsNilWhenNotFound(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewContentStore(db)

	row, err := store.GetFileContent(context.Background(), "repo-1", "missing.go")
	if err != nil {
		t.Fatalf("GetFileContent() error = %v, want nil", err)
	}
	if row != nil {
		t.Fatalf("GetFileContent() = %v, want nil", row)
	}
}

func TestContentStoreGetFileContentPropagatesQueryError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{err: errors.New("db down")}},
	}
	store := NewContentStore(db)

	_, err := store.GetFileContent(context.Background(), "repo-1", "main.go")
	if err == nil {
		t.Fatal("GetFileContent() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "get file content") {
		t.Fatalf("error = %q, want 'get file content' context", err)
	}
}

func TestContentStoreGetFileContentRequiresDB(t *testing.T) {
	t.Parallel()

	store := ContentStore{}

	_, err := store.GetFileContent(context.Background(), "repo-1", "main.go")
	if err == nil {
		t.Fatal("GetFileContent() error = nil, want non-nil")
	}
}

// --- GetEntityContent tests ---

func TestContentStoreGetEntityContentReturnsRow(t *testing.T) {
	t.Parallel()

	startByte := 0
	endByte := 100
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{contentEntityRow("entity-1", "repo-1", "main.go", "function", "main", 1, 10, &startByte, &endByte, "go", "source", "", nil, "func main() {}")}},
		},
	}
	store := NewContentStore(db)

	row, err := store.GetEntityContent(context.Background(), "entity-1")
	if err != nil {
		t.Fatalf("GetEntityContent() error = %v, want nil", err)
	}
	if row == nil {
		t.Fatal("GetEntityContent() = nil, want non-nil")
	}
	if got, want := row.EntityID, "entity-1"; got != want {
		t.Fatalf("EntityID = %q, want %q", got, want)
	}
	if got, want := row.EntityType, "function"; got != want {
		t.Fatalf("EntityType = %q, want %q", got, want)
	}
	if got, want := row.SourceCache, "func main() {}"; got != want {
		t.Fatalf("SourceCache = %q, want %q", got, want)
	}
	if row.StartByte == nil || *row.StartByte != 0 {
		t.Fatalf("StartByte = %v, want 0", row.StartByte)
	}
	if row.EndByte == nil || *row.EndByte != 100 {
		t.Fatalf("EndByte = %v, want 100", row.EndByte)
	}
}

func TestContentStoreGetEntityContentReturnsNilWhenNotFound(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewContentStore(db)

	row, err := store.GetEntityContent(context.Background(), "missing-entity")
	if err != nil {
		t.Fatalf("GetEntityContent() error = %v, want nil", err)
	}
	if row != nil {
		t.Fatalf("GetEntityContent() = %v, want nil", row)
	}
}

func TestContentStoreGetEntityContentPropagatesQueryError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{err: errors.New("db down")}},
	}
	store := NewContentStore(db)

	_, err := store.GetEntityContent(context.Background(), "entity-1")
	if err == nil {
		t.Fatal("GetEntityContent() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "get entity content") {
		t.Fatalf("error = %q, want 'get entity content' context", err)
	}
}

// --- SearchFileContent tests ---

func TestContentStoreSearchFileContentReturnsMatches(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				contentFileRow("repo-1", "main.go", "", "func main() {}", "hash1", 1, "go", "", "", nil),
				contentFileRow("repo-1", "util.go", "", "func helper() {}", "hash2", 1, "go", "", "", nil),
			}},
		},
	}
	store := NewContentStore(db)

	results, err := store.SearchFileContent(context.Background(), "func", "repo-1", 50)
	if err != nil {
		t.Fatalf("SearchFileContent() error = %v, want nil", err)
	}
	if got, want := len(results), 2; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	if got, want := results[0].RelativePath, "main.go"; got != want {
		t.Fatalf("results[0].RelativePath = %q, want %q", got, want)
	}
}

func TestContentStoreSearchFileContentBuildsRepoFilter(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewContentStore(db)

	_, err := store.SearchFileContent(context.Background(), "func", "repo-1", 50)
	if err != nil {
		t.Fatalf("SearchFileContent() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "repo_id = $2") {
		t.Fatalf("query = %q, want repo_id filter", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "ILIKE $1") {
		t.Fatalf("query = %q, want ILIKE filter", db.queries[0].query)
	}
}

func TestContentStoreSearchFileContentOmitsRepoFilterWhenEmpty(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewContentStore(db)

	_, err := store.SearchFileContent(context.Background(), "func", "", 50)
	if err != nil {
		t.Fatalf("SearchFileContent() error = %v, want nil", err)
	}
	if strings.Contains(db.queries[0].query, "repo_id =") {
		t.Fatalf("query = %q, want no repo_id filter", db.queries[0].query)
	}
}

func TestContentStoreSearchFileContentDefaultsLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	store := NewContentStore(db)

	_, err := store.SearchFileContent(context.Background(), "func", "", 0)
	if err != nil {
		t.Fatalf("SearchFileContent() error = %v, want nil", err)
	}
	// With no repo filter: $1 = pattern, $2 = limit (50)
	if got, want := db.queries[0].args[1], 50; got != want {
		t.Fatalf("limit arg = %v, want %v", got, want)
	}
}

// --- SearchEntityContent tests ---

func TestContentStoreSearchEntityContentReturnsMatches(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				contentEntityRow("entity-1", "repo-1", "main.go", "function", "main", 1, 10, nil, nil, "go", "", "", nil, "func main() {}"),
			}},
		},
	}
	store := NewContentStore(db)

	results, err := store.SearchEntityContent(context.Background(), "func", "repo-1", 50)
	if err != nil {
		t.Fatalf("SearchEntityContent() error = %v, want nil", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	if got, want := results[0].EntityID, "entity-1"; got != want {
		t.Fatalf("results[0].EntityID = %q, want %q", got, want)
	}
}

func TestContentStoreSearchEntityContentPropagatesQueryError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{err: errors.New("timeout")}},
	}
	store := NewContentStore(db)

	_, err := store.SearchEntityContent(context.Background(), "func", "", 10)
	if err == nil {
		t.Fatal("SearchEntityContent() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "search entity content") {
		t.Fatalf("error = %q, want 'search entity content' context", err)
	}
}

// --- UpsertFileBatch tests ---

func TestContentStoreUpsertFileBatchInsertsRows(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewContentStore(db)

	files := []content.Record{
		{Path: "main.go", Body: "package main\n", Metadata: map[string]string{"language": "go"}},
		{Path: "util.go", Body: "package util\n", Metadata: map[string]string{"language": "go"}},
	}

	if err := store.UpsertFileBatch(context.Background(), "repo-1", files); err != nil {
		t.Fatalf("UpsertFileBatch() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	for _, exec := range db.execs {
		if !strings.Contains(exec.query, "INSERT INTO content_files") {
			t.Fatalf("query = %q, want content_files insert", exec.query)
		}
	}
}

func TestContentStoreUpsertFileBatchDeletesTombstoned(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewContentStore(db)

	files := []content.Record{
		{Path: "deleted.go", Deleted: true},
	}

	if err := store.UpsertFileBatch(context.Background(), "repo-1", files); err != nil {
		t.Fatalf("UpsertFileBatch() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d (entity delete + file delete)", got, want)
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM content_entities") {
		t.Fatalf("exec[0] = %q, want entity delete", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "DELETE FROM content_files") {
		t.Fatalf("exec[1] = %q, want file delete", db.execs[1].query)
	}
}

func TestContentStoreUpsertFileBatchRejectsEmptyRepoID(t *testing.T) {
	t.Parallel()

	store := NewContentStore(&fakeExecQueryer{})

	err := store.UpsertFileBatch(context.Background(), "", []content.Record{{Path: "a.go"}})
	if err == nil {
		t.Fatal("UpsertFileBatch() error = nil, want non-nil")
	}
}

func TestContentStoreUpsertFileBatchRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	store := NewContentStore(&fakeExecQueryer{})

	err := store.UpsertFileBatch(context.Background(), "repo-1", []content.Record{{Path: ""}})
	if err == nil {
		t.Fatal("UpsertFileBatch() error = nil, want non-nil")
	}
}

func TestContentStoreUpsertFileBatchSkipsEmpty(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewContentStore(db)

	if err := store.UpsertFileBatch(context.Background(), "repo-1", nil); err != nil {
		t.Fatalf("UpsertFileBatch() error = %v, want nil", err)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0", got)
	}
}

// --- UpsertEntityBatch tests ---

func TestContentStoreUpsertEntityBatchInsertsRows(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewContentStore(db)

	entities := []content.EntityRecord{
		{
			EntityID:   "entity-1",
			Path:       "main.go",
			EntityType: "function",
			EntityName: "main",
			StartLine:  1,
			EndLine:    10,
		},
	}

	if err := store.UpsertEntityBatch(context.Background(), "repo-1", entities); err != nil {
		t.Fatalf("UpsertEntityBatch() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO content_entities") {
		t.Fatalf("query = %q, want content_entities insert", db.execs[0].query)
	}
}

func TestContentStoreUpsertEntityBatchDeletesTombstoned(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewContentStore(db)

	entities := []content.EntityRecord{
		{
			EntityID:   "entity-1",
			Path:       "main.go",
			EntityType: "function",
			EntityName: "main",
			StartLine:  1,
			Deleted:    true,
		},
	}

	if err := store.UpsertEntityBatch(context.Background(), "repo-1", entities); err != nil {
		t.Fatalf("UpsertEntityBatch() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM content_entities") {
		t.Fatalf("query = %q, want entity delete", db.execs[0].query)
	}
}

func TestContentStoreUpsertEntityBatchRejectsEmptyRepoID(t *testing.T) {
	t.Parallel()

	store := NewContentStore(&fakeExecQueryer{})

	err := store.UpsertEntityBatch(context.Background(), "", []content.EntityRecord{
		{EntityID: "e1", Path: "a.go", EntityType: "function", EntityName: "f", StartLine: 1},
	})
	if err == nil {
		t.Fatal("UpsertEntityBatch() error = nil, want non-nil")
	}
}

func TestContentStoreUpsertEntityBatchRejectsInvalidEntity(t *testing.T) {
	t.Parallel()

	store := NewContentStore(&fakeExecQueryer{})

	err := store.UpsertEntityBatch(context.Background(), "repo-1", []content.EntityRecord{
		{EntityID: "", Path: "a.go", EntityType: "function", EntityName: "f", StartLine: 1},
	})
	if err == nil {
		t.Fatal("UpsertEntityBatch() error = nil, want non-nil for empty entity ID")
	}
}

func TestContentStoreUpsertEntityBatchSkipsEmpty(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewContentStore(db)

	if err := store.UpsertEntityBatch(context.Background(), "repo-1", nil); err != nil {
		t.Fatalf("UpsertEntityBatch() error = %v, want nil", err)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0", got)
	}
}

// --- Query builder tests ---

func TestBuildFileSearchQueryWithRepoFilter(t *testing.T) {
	t.Parallel()

	query, args := buildFileSearchQuery("hello", "repo-1", 25)
	if !strings.Contains(query, "ILIKE $1") {
		t.Fatalf("query missing ILIKE: %s", query)
	}
	if !strings.Contains(query, "repo_id = $2") {
		t.Fatalf("query missing repo filter: %s", query)
	}
	if !strings.Contains(query, "LIMIT $3") {
		t.Fatalf("query missing LIMIT: %s", query)
	}
	if got, want := args[0], "%hello%"; got != want {
		t.Fatalf("args[0] = %v, want %v", got, want)
	}
	if got, want := args[1], "repo-1"; got != want {
		t.Fatalf("args[1] = %v, want %v", got, want)
	}
	if got, want := args[2], 25; got != want {
		t.Fatalf("args[2] = %v, want %v", got, want)
	}
}

func TestBuildFileSearchQueryWithoutRepoFilter(t *testing.T) {
	t.Parallel()

	query, args := buildFileSearchQuery("hello", "", 10)
	if strings.Contains(query, "repo_id = $") {
		t.Fatalf("query should not contain repo_id WHERE filter: %s", query)
	}
	if !strings.Contains(query, "LIMIT $2") {
		t.Fatalf("query missing LIMIT: %s", query)
	}
	if got, want := len(args), 2; got != want {
		t.Fatalf("args len = %d, want %d", got, want)
	}
}

func TestBuildEntitySearchQueryWithRepoFilter(t *testing.T) {
	t.Parallel()

	query, args := buildEntitySearchQuery("func", "repo-1", 30)
	if !strings.Contains(query, "source_cache ILIKE $1") {
		t.Fatalf("query missing source_cache ILIKE: %s", query)
	}
	if !strings.Contains(query, "ce.repo_id = $2") {
		t.Fatalf("query missing repo filter: %s", query)
	}
	if got, want := len(args), 3; got != want {
		t.Fatalf("args len = %d, want %d", got, want)
	}
}

// --- test helpers ---

// contentFileRow builds a fake row slice matching the scanFileContentRow column order.
// Nullable columns are encoded as sql.Null* types so the existing fakeExecQueryer
// Scan implementation can handle them.
func contentFileRow(
	repoID, relativePath, commitSHA, body, contentHash string,
	lines int,
	language, artifactType, templateDialect string,
	iacRelevant *bool,
) []any {
	return []any{
		repoID,
		relativePath,
		toNullString(commitSHA),
		body,
		contentHash,
		lines,
		toNullString(language),
		toNullString(artifactType),
		toNullString(templateDialect),
		toNullBool(iacRelevant),
	}
}

// contentEntityRow builds a fake row slice matching the scanEntityContentRow column order.
func contentEntityRow(
	entityID, repoID, relativePath, entityType, entityName string,
	startLine, endLine int,
	startByte, endByte *int,
	language, artifactType, templateDialect string,
	iacRelevant *bool,
	sourceCache string,
) []any {
	return []any{
		entityID,
		repoID,
		relativePath,
		entityType,
		entityName,
		startLine,
		endLine,
		toNullInt64(startByte),
		toNullInt64(endByte),
		toNullString(language),
		toNullString(artifactType),
		toNullString(templateDialect),
		toNullBool(iacRelevant),
		sourceCache,
	}
}

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func toNullBool(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

func toNullInt64(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}
