package shape

import (
	"strings"
	"testing"
)

// TestMaterializePerFileEntityCapSkipsExcessEntities verifies that when a
// single file produces more than MaxFileEntityCount entities, entity
// materialization is skipped entirely for that file and the entity list is
// empty. The content record (file body/digest) is still emitted.
//
// Regression: ckeditor.js produced 24,720 entities and one PHP class file
// produced 53,830 from a single file (issue #3676). Files with extremely high
// entity counts are minified, generated, or pathological and contribute noise
// to BM25/search indexing.
func TestMaterializePerFileEntityCapSkipsExcessEntities(t *testing.T) {
	t.Parallel()

	// Build more entities than MaxFileEntityCount to trigger the cap.
	entities := make([]Entity, 0, MaxFileEntityCount+1)
	for i := 0; i < MaxFileEntityCount+1; i++ {
		entities = append(entities, Entity{
			Name:       "fn" + itoa(i),
			LineNumber: i + 1,
		})
	}

	got, err := Materialize(Input{
		RepoID:       "repository:r_test0010",
		SourceSystem: "git",
		Files: []File{
			{
				Path:     "ckeditor/ckeditor.js",
				Language: "javascript",
				Body:     "/* minified */\n",
				EntityBuckets: map[string][]Entity{
					"functions": entities,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	// Content record must still be emitted.
	if got, want := len(got.Records), 1; got != want {
		t.Fatalf("record count = %d, want %d (file record must survive entity cap)", got, want)
	}

	// Entity list must be empty — all entities dropped.
	if len(got.Entities) > 0 {
		t.Fatalf("entity count = %d, want 0 for file exceeding MaxFileEntityCount (%d)",
			len(got.Entities), MaxFileEntityCount)
	}
}

// TestMaterializePerFileEntityCapPreservesNormalFiles verifies that files with
// entity counts at or below MaxFileEntityCount are not affected by the cap.
func TestMaterializePerFileEntityCapPreservesNormalFiles(t *testing.T) {
	t.Parallel()

	const wantCount = 100
	entities := make([]Entity, 0, wantCount)
	for i := 0; i < wantCount; i++ {
		entities = append(entities, Entity{
			Name:       "Widget" + itoa(i),
			LineNumber: i + 1,
		})
	}

	got, err := Materialize(Input{
		RepoID:       "repository:r_test0011",
		SourceSystem: "git",
		Files: []File{
			{
				Path:     "src/widget.go",
				Language: "go",
				EntityBuckets: map[string][]Entity{
					"classes": entities,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if got, want := len(got.Entities), wantCount; got != want {
		t.Fatalf("entity count = %d, want %d (normal file must not be capped)", got, want)
	}
}

// TestMaterializeMinifiedJSFileSkippedByEntityCap verifies that a file whose
// name contains ".min." and which produces excess entities is correctly handled
// by the per-file entity cap.
func TestMaterializeMinifiedJSFileSkippedByEntityCap(t *testing.T) {
	t.Parallel()

	entities := make([]Entity, 0, MaxFileEntityCount+100)
	for i := 0; i < MaxFileEntityCount+100; i++ {
		entities = append(entities, Entity{
			Name:       "f" + itoa(i),
			LineNumber: i + 1,
		})
	}

	got, err := Materialize(Input{
		RepoID:       "repository:r_test0012",
		SourceSystem: "git",
		Files: []File{
			{
				Path:     "assets/vendor.min.js",
				Language: "javascript",
				Body:     strings.Repeat("a", 1024),
				EntityBuckets: map[string][]Entity{
					"functions": entities,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if len(got.Entities) > 0 {
		t.Fatalf("entity count = %d, want 0 for minified file exceeding MaxFileEntityCount",
			len(got.Entities))
	}

	// Content record must still be present.
	if got, want := len(got.Records), 1; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
}
