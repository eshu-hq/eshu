package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathGoEmbeddedSQLQueries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "repo.go")
	writeTestFile(
		t,
		filePath,
		`package repo

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
)

func listUsers(db *sql.DB) error {
	_, err := db.Exec("UPDATE public.users SET email = email WHERE id = $1", 42)
	return err
}

func loadOrgs(db *sqlx.DB) error {
	_, err := db.Queryx("SELECT id FROM public.orgs WHERE id = $1", 42)
	return err
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	queries, ok := got["embedded_sql_queries"].([]map[string]any)
	if !ok {
		t.Fatalf("embedded_sql_queries = %T, want []map[string]any", got["embedded_sql_queries"])
	}

	want := []map[string]any{
		{
			"function_name":        "listUsers",
			"function_line_number": 9,
			"table_name":           "public.users",
			"operation":            "update",
			"line_number":          10,
			"api":                  "database/sql",
		},
		{
			"function_name":        "loadOrgs",
			"function_line_number": 14,
			"table_name":           "public.orgs",
			"operation":            "select",
			"line_number":          15,
			"api":                  "sqlx",
		},
	}

	if !reflect.DeepEqual(queries, want) {
		t.Fatalf("embedded_sql_queries = %#v, want %#v", queries, want)
	}
}

func TestIterGoStringLiteralsPreservesEscapedQuotes(t *testing.T) {
	t.Parallel()

	got := iterGoStringLiterals(
		`db.Exec("SELECT id /* \"audit\" */ FROM public.users WHERE id = $1", 42)`,
	)

	want := []goStringLiteral{
		{
			body:   `SELECT id /* \"audit\" */ FROM public.users WHERE id = $1`,
			offset: 9,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("iterGoStringLiterals() = %#v, want %#v", got, want)
	}
}
