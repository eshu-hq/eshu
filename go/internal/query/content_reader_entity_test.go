package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

func TestContentReaderGetEntityContentIncludesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/app.ts", "Function", "renderApp",
					int64(10), int64(24), "typescript", "function renderApp() {}", []byte(`{"docstring":"Renders the app.","decorators":["component"],"async":true}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	entity, err := reader.GetEntityContent(context.Background(), "entity-1")
	if err != nil {
		t.Fatalf("GetEntityContent() error = %v, want nil", err)
	}
	if entity == nil {
		t.Fatal("GetEntityContent() = nil, want non-nil")
	}

	if got, want := entity.Metadata["docstring"], "Renders the app."; got != want {
		t.Fatalf("Metadata[docstring] = %#v, want %#v", got, want)
	}
	if got, want := entity.Metadata["async"], true; got != want {
		t.Fatalf("Metadata[async] = %#v, want %#v", got, want)
	}

	decorators, ok := entity.Metadata["decorators"].([]any)
	if !ok {
		t.Fatalf("Metadata[decorators] type = %T, want []any", entity.Metadata["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "component" {
		t.Fatalf("Metadata[decorators] = %#v, want [component]", decorators)
	}
}

func TestContentReaderGetEntityContentsIncludesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/app.ts", "Function", "renderApp",
					int64(10), int64(24), "typescript", "function renderApp() {}", []byte(`{"async":true}`),
				},
				{
					"entity-2", "repo-1", "src/service.ts", "Class", "PaymentService",
					int64(3), int64(40), "typescript", "class PaymentService {}", []byte(`{"decorators":["Injectable"]}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	entities, err := reader.GetEntityContents(context.Background(), []string{"entity-1", "entity-2", "entity-1", ""})
	if err != nil {
		t.Fatalf("GetEntityContents() error = %v, want nil", err)
	}
	if got, want := len(entities), 2; got != want {
		t.Fatalf("len(entities) = %d, want %d", got, want)
	}
	if got, want := entities["entity-1"].Metadata["async"], true; got != want {
		t.Fatalf("entity-1 Metadata[async] = %#v, want %#v", got, want)
	}
	decorators, ok := entities["entity-2"].Metadata["decorators"].([]any)
	if !ok {
		t.Fatalf("entity-2 Metadata[decorators] type = %T, want []any", entities["entity-2"].Metadata["decorators"])
	}
	if got, want := decorators[0], "Injectable"; got != want {
		t.Fatalf("entity-2 decorator = %#v, want %#v", got, want)
	}
}

func TestContentReaderSearchEntityContentIncludesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/app.tsx", "Function", "renderApp",
					int64(10), int64(24), "tsx", "function renderApp() {}", []byte(`{"method_kind":"component","jsx_component_usage":["Button"]}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	results, err := reader.SearchEntityContent(context.Background(), "repo-1", "render", 10)
	if err != nil {
		t.Fatalf("SearchEntityContent() error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	if got, want := results[0].Metadata["method_kind"], "component"; got != want {
		t.Fatalf("Metadata[method_kind] = %#v, want %#v", got, want)
	}
	usage, ok := results[0].Metadata["jsx_component_usage"].([]any)
	if !ok {
		t.Fatalf("Metadata[jsx_component_usage] type = %T, want []any", results[0].Metadata["jsx_component_usage"])
	}
	if len(usage) != 1 || usage[0] != "Button" {
		t.Fatalf("Metadata[jsx_component_usage] = %#v, want [Button]", usage)
	}
}

func TestContentReaderListRepoEntitiesIncludesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"alias-1", "repo-1", "src/types.ts", "TypeAlias", "UserID",
					int64(3), int64(3), "typescript", "type UserID = string", []byte(`{"type":"string","type_parameters":["T"]}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	results, err := reader.ListRepoEntities(context.Background(), "repo-1", 10)
	if err != nil {
		t.Fatalf("ListRepoEntities() error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	if got, want := results[0].Metadata["type"], "string"; got != want {
		t.Fatalf("Metadata[type] = %#v, want %#v", got, want)
	}
	params, ok := results[0].Metadata["type_parameters"].([]any)
	if !ok {
		t.Fatalf("Metadata[type_parameters] type = %T, want []any", results[0].Metadata["type_parameters"])
	}
	if len(params) != 1 || params[0] != "T" {
		t.Fatalf("Metadata[type_parameters] = %#v, want [T]", params)
	}
}

func TestContentReaderGetEntityContentRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/app.py", "Function", "handler",
					int64(1), int64(5), "python", "def handler(): pass", []byte(`{bad json}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	_, err := reader.GetEntityContent(context.Background(), "entity-1")
	if err == nil {
		t.Fatal("GetEntityContent() error = nil, want non-nil")
	}
}

func TestContentReaderSearchEntitiesByLanguageAndTypeIncludesLanguageVariants(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"component-1", "repo-1", "src/Button.tsx", "Component", "Button",
					int64(1), int64(10), "tsx", "export function Button() {}", []byte(`{"framework":"react"}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	results, err := reader.SearchEntitiesByLanguageAndType(context.Background(), "repo-1", "typescript", "Component", "Button", 10)
	if err != nil {
		t.Fatalf("SearchEntitiesByLanguageAndType() error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if got, want := results[0].Language, "tsx"; got != want {
		t.Fatalf("results[0].Language = %#v, want %#v", got, want)
	}
	if got, want := results[0].Metadata["framework"], "react"; got != want {
		t.Fatalf("results[0].Metadata[framework] = %#v, want %#v", got, want)
	}
}

func TestContentReaderDeadCodeCandidateRowsReturnsGraphShapedRows(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: contentReaderDeadCodeCandidateColumns(),
			rows: [][]driver.Value{
				{
					"entity-1", "deadHelper", "Function", "repo-1", "src/dead.go",
					"go", int64(12), int64(18), []byte(`{"dead_code_root_kinds":["none"]}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	rows, err := reader.DeadCodeCandidateRows(context.Background(), "repo-1", "Function", "go", 10, 0)
	if err != nil {
		t.Fatalf("DeadCodeCandidateRows() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got, want := row["entity_id"], "entity-1"; got != want {
		t.Fatalf("row[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := row["file_path"], "src/dead.go"; got != want {
		t.Fatalf("row[file_path] = %#v, want %#v", got, want)
	}
	labels, ok := row["labels"].([]any)
	if !ok {
		t.Fatalf("row[labels] type = %T, want []any", row["labels"])
	}
	if len(labels) != 1 || labels[0] != "Function" {
		t.Fatalf("row[labels] = %#v, want [Function]", labels)
	}
	metadata, ok := row["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("row[metadata] type = %T, want map[string]any", row["metadata"])
	}
	rootKinds, ok := metadata["dead_code_root_kinds"].([]any)
	if !ok || len(rootKinds) != 1 || rootKinds[0] != "none" {
		t.Fatalf("metadata[dead_code_root_kinds] = %#v, want [none]", metadata["dead_code_root_kinds"])
	}
}

func TestContentReaderDeadCodeCandidateRowsReturnsSQLFunctions(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: contentReaderDeadCodeCandidateColumns(),
			rows: [][]driver.Value{
				{
					"sql-refresh", "public.refresh_users", "SqlFunction", "repo-1", "db/functions.sql",
					"sql", int64(3), int64(12), []byte(`{"sql_dialect":"postgres"}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	rows, err := reader.DeadCodeCandidateRows(context.Background(), "repo-1", "SqlFunction", "sql", 10, 0)
	if err != nil {
		t.Fatalf("DeadCodeCandidateRows() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got, want := row["entity_id"], "sql-refresh"; got != want {
		t.Fatalf("row[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := row["language"], "sql"; got != want {
		t.Fatalf("row[language] = %#v, want %#v", got, want)
	}
	labels, ok := row["labels"].([]any)
	if !ok {
		t.Fatalf("row[labels] type = %T, want []any", row["labels"])
	}
	if len(labels) != 1 || labels[0] != "SqlFunction" {
		t.Fatalf("row[labels] = %#v, want [SqlFunction]", labels)
	}
	metadata, ok := row["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("row[metadata] type = %T, want map[string]any", row["metadata"])
	}
	if got, want := metadata["sql_dialect"], "postgres"; got != want {
		t.Fatalf("metadata[sql_dialect] = %#v, want %#v", got, want)
	}
}

func TestContentReaderDeadCodeCandidateRowsAllowsRepositoryOptionalScan(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: contentReaderDeadCodeCandidateColumns(),
			rows: [][]driver.Value{
				{
					"sql-refresh", "public.refresh_users", "SqlFunction", "repo-2", "db/functions.sql",
					"sql", int64(3), int64(12), []byte(`{}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	rows, err := reader.DeadCodeCandidateRows(context.Background(), "", "SqlFunction", "sql", 10, 0)
	if err != nil {
		t.Fatalf("DeadCodeCandidateRows() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0]["repo_id"], "repo-2"; got != want {
		t.Fatalf("row[repo_id] = %#v, want %#v", got, want)
	}
}
