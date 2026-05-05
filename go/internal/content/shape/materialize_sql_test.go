package shape

import "testing"

func TestMaterializeBuildsSQLEntities(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID:       "repository:r_sql",
		SourceSystem: "git",
		Files: []File{
			{
				Path:     "schema.sql",
				Body:     "CREATE TABLE public.audit_logs (id BIGSERIAL PRIMARY KEY);",
				Digest:   "digest-sql",
				Language: "sql",
				EntityBuckets: map[string][]Entity{
					"sql_tables": {
						{
							Name:       "public.audit_logs",
							LineNumber: 1,
							Metadata: map[string]any{
								"schema":         "public",
								"qualified_name": "public.audit_logs",
							},
						},
					},
					"sql_functions": {
						{
							Name:       "public.archive_audit",
							LineNumber: 3,
							Metadata: map[string]any{
								"schema":            "public",
								"qualified_name":    "public.archive_audit",
								"function_language": "plpgsql",
								"routine_kind":      "procedure",
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if len(got.Entities) != 2 {
		t.Fatalf("len(Materialize().Entities) = %d, want 2", len(got.Entities))
	}

	table := got.Entities[0]
	if got, want := table.EntityType, "SqlTable"; got != want {
		t.Fatalf("entities[0].EntityType = %q, want %q", got, want)
	}
	if got, want := table.Metadata["qualified_name"], "public.audit_logs"; got != want {
		t.Fatalf("entities[0].Metadata[qualified_name] = %#v, want %#v", got, want)
	}

	procedure := got.Entities[1]
	if got, want := procedure.EntityType, "SqlFunction"; got != want {
		t.Fatalf("entities[1].EntityType = %q, want %q", got, want)
	}
	if got, want := procedure.Metadata["routine_kind"], "procedure"; got != want {
		t.Fatalf("entities[1].Metadata[routine_kind] = %#v, want %#v", got, want)
	}
	if got, want := procedure.Metadata["function_language"], "plpgsql"; got != want {
		t.Fatalf("entities[1].Metadata[function_language] = %#v, want %#v", got, want)
	}
}
