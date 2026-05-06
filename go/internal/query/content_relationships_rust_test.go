package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

func TestBuildContentRelationshipSetRustImplBlockContainsMethods(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"fn-new", "repo-1", "src/point.rs", "Function", "new",
					int64(3), int64(7), "rust", "fn new() -> Self { Self {} }", []byte(`{"impl_context":"Point"}`),
				},
				{
					"fn-x", "repo-1", "src/point.rs", "Function", "x",
					int64(9), int64(13), "rust", "fn x(&self) -> i32 { self.x }", []byte(`{"impl_context":"Point"}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	implBlock := EntityContent{
		EntityID:     "impl-1",
		RepoID:       "repo-1",
		RelativePath: "src/point.rs",
		EntityType:   "ImplBlock",
		EntityName:   "Point",
		Language:     "rust",
		Metadata: map[string]any{
			"kind":   "trait_impl",
			"trait":  "Display",
			"target": "Point",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, implBlock)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 2 {
		t.Fatalf("len(relationships.outgoing) = %d, want 2", len(relationships.outgoing))
	}

	relationship := relationships.outgoing[0]
	if got, want := relationship["type"], "CONTAINS"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "new"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_id"], "fn-new"; got != want {
		t.Fatalf("relationship[target_id] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "rust_impl_context"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetRustFunctionBelongsToImplBlock(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"impl-1", "repo-1", "src/point.rs", "ImplBlock", "Point",
					int64(1), int64(18), "rust", "impl Display for Point {}", []byte(`{"kind":"trait_impl","trait":"Display","target":"Point"}`),
				},
			},
		},
	})

	reader := NewContentReader(db)
	fn := EntityContent{
		EntityID:     "fn-new",
		RepoID:       "repo-1",
		RelativePath: "src/point.rs",
		EntityType:   "Function",
		EntityName:   "new",
		Language:     "rust",
		Metadata: map[string]any{
			"impl_context": "Point",
		},
	}

	relationships, err := buildContentRelationshipSet(context.Background(), reader, fn)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 0 {
		t.Fatalf("len(relationships.outgoing) = %d, want 0", len(relationships.outgoing))
	}
	if len(relationships.incoming) != 1 {
		t.Fatalf("len(relationships.incoming) = %d, want 1", len(relationships.incoming))
	}

	relationship := relationships.incoming[0]
	if got, want := relationship["type"], "CONTAINS"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["source_name"], "Point"; got != want {
		t.Fatalf("relationship[source_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["source_id"], "impl-1"; got != want {
		t.Fatalf("relationship[source_id] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "rust_impl_context"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
}
