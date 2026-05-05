package content

import (
	"context"
	"testing"
)

func TestMaterializationScopeGenerationKey(t *testing.T) {
	t.Parallel()

	got := Materialization{
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
	}

	if want := "scope-123:generation-456"; got.ScopeGenerationKey() != want {
		t.Fatalf("Materialization.ScopeGenerationKey() = %q, want %q", got.ScopeGenerationKey(), want)
	}
}

func TestMaterializationCloneCopiesRecords(t *testing.T) {
	t.Parallel()

	original := Materialization{
		RepoID:       "repository:r_12345678",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Records: []Record{
			{
				Path:     "README.md",
				Body:     "hello",
				Digest:   "digest-1",
				Deleted:  false,
				Metadata: map[string]string{"language": "markdown"},
			},
		},
	}

	cloned := original.Clone()
	if got, want := cloned.RepoID, "repository:r_12345678"; got != want {
		t.Fatalf("cloned RepoID = %q, want %q", got, want)
	}
	cloned.Records[0].Metadata["language"] = "mutated"

	if got, want := original.Records[0].Metadata["language"], "markdown"; got != want {
		t.Fatalf("original record metadata = %q, want %q", got, want)
	}
}

func TestMaterializationCloneCopiesEntityRecords(t *testing.T) {
	t.Parallel()

	original := Materialization{
		RepoID:       "repository:r_12345678",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Entities: []EntityRecord{
			{
				EntityID:        "content-entity:e_ab12cd34ef56",
				Path:            "schema.sql",
				EntityType:      "SqlTable",
				EntityName:      "public.users",
				StartLine:       10,
				EndLine:         20,
				Language:        "sql",
				SourceCache:     "create table public.users",
				TemplateDialect: "ansi",
				Metadata: map[string]any{
					"docstring":   "table docs",
					"decorators":  []string{"@tracked"},
					"nested_data": map[string]any{"owner": "data-platform"},
				},
			},
		},
	}

	cloned := original.Clone()
	cloned.Entities[0].EntityName = "mutated"
	cloned.Entities[0].Metadata["docstring"] = "mutated"
	nested := cloned.Entities[0].Metadata["nested_data"].(map[string]any)
	nested["owner"] = "mutated"

	if got, want := original.Entities[0].EntityName, "public.users"; got != want {
		t.Fatalf("original entity name = %q, want %q", got, want)
	}
	if got, want := original.Entities[0].Metadata["docstring"], "table docs"; got != want {
		t.Fatalf("original entity metadata docstring = %#v, want %#v", got, want)
	}
	originalNested := original.Entities[0].Metadata["nested_data"].(map[string]any)
	if got, want := originalNested["owner"], "data-platform"; got != want {
		t.Fatalf("original entity nested metadata owner = %#v, want %#v", got, want)
	}
}

func TestMemoryWriterStoresClone(t *testing.T) {
	t.Parallel()

	writer := &MemoryWriter{}
	got, err := writer.Write(context.Background(), Materialization{
		RepoID:       "repository:r_12345678",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		Records: []Record{{
			Path:   "README.md",
			Body:   "hello",
			Digest: "digest-1",
		}},
		Entities: []EntityRecord{{
			EntityID:    "content-entity:e_ab12cd34ef56",
			Path:        "README.md",
			EntityType:  "Function",
			EntityName:  "hello",
			StartLine:   1,
			EndLine:     1,
			SourceCache: "func hello() {}\n",
			Metadata: map[string]any{
				"docstring": "Greets callers.",
			},
		}},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if got.RecordCount != 1 {
		t.Fatalf("Write().RecordCount = %d, want 1", got.RecordCount)
	}
	if got.EntityCount != 1 {
		t.Fatalf("Write().EntityCount = %d, want 1", got.EntityCount)
	}
	if got, want := writer.Writes[0].RepoID, "repository:r_12345678"; got != want {
		t.Fatalf("stored RepoID = %q, want %q", got, want)
	}
	if got, want := writer.Writes[0].Entities[0].EntityID, "content-entity:e_ab12cd34ef56"; got != want {
		t.Fatalf("stored EntityID = %q, want %q", got, want)
	}
	if got, want := writer.Writes[0].Entities[0].Metadata["docstring"], "Greets callers."; got != want {
		t.Fatalf("stored entity metadata docstring = %#v, want %#v", got, want)
	}
}

func TestCanonicalEntityIDIsStableAndPrefixed(t *testing.T) {
	t.Parallel()

	got := CanonicalEntityID(
		"repository:r_12345678",
		"schema.sql",
		"SqlTable",
		"public.users",
		10,
	)

	if got == "" {
		t.Fatal("CanonicalEntityID() = empty string, want stable identifier")
	}
	if got != CanonicalEntityID("repository:r_12345678", "schema.sql", "SqlTable", "public.users", 10) {
		t.Fatalf("CanonicalEntityID() = %q, want stable output", got)
	}
	if want := "content-entity:e_4c49e9b3dd77"; got != want {
		t.Fatalf("CanonicalEntityID() = %q, want %q", got, want)
	}
	if wantPrefix := "content-entity:e_"; len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("CanonicalEntityID() = %q, want prefix %q", got, wantPrefix)
	}
}
