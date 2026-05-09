package golang

import (
	"reflect"
	"testing"
)

func TestEmbeddedSQLQueries(t *testing.T) {
	t.Parallel()

	source := `package repo

func listUsers(db databaseSQL) error {
	_, err := db.Exec("UPDATE public.users SET email = email WHERE id = $1", 42)
	return err
}

func loadOrgs(db sqlxDB) error {
	_, err := db.Queryx(` + "`" + `SELECT id FROM public.orgs WHERE id = $1` + "`" + `, 42)
	return err
}
`

	got := EmbeddedSQLQueries(source)
	want := []EmbeddedSQLQuery{
		{
			FunctionName:       "listUsers",
			FunctionLineNumber: 3,
			TableName:          "public.users",
			Operation:          "update",
			LineNumber:         4,
			API:                "database/sql",
		},
		{
			FunctionName:       "loadOrgs",
			FunctionLineNumber: 8,
			TableName:          "public.orgs",
			Operation:          "select",
			LineNumber:         9,
			API:                "sqlx",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EmbeddedSQLQueries() = %#v, want %#v", got, want)
	}
}

func TestEmbeddedSQLQueriesPreservesEscapedQuotes(t *testing.T) {
	t.Parallel()

	source := `package repo

func loadUsers(db databaseSQL) {
	db.Exec("SELECT id /* \"audit\" */ FROM public.users WHERE id = $1", 42)
}
`

	got := EmbeddedSQLQueries(source)
	if len(got) != 1 {
		t.Fatalf("EmbeddedSQLQueries() len = %d, want 1: %#v", len(got), got)
	}
	if got[0].TableName != "public.users" {
		t.Fatalf("EmbeddedSQLQueries()[0].TableName = %q, want public.users", got[0].TableName)
	}
}
