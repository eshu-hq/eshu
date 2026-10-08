// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// metaclassScopeFileEnvelope builds one "file" fact carrying class items for
// the #7642 metaclass scope tests.
func metaclassScopeFileEnvelope(repoID, rawPath, relativePath string, classes ...any) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path":    rawPath,
				"classes": classes,
			},
		},
	}
}

func metaclassScopeRepoEnvelope(repoID string) facts.Envelope {
	return facts.Envelope{
		FactKind: "repository",
		Payload:  map[string]any{"repo_id": repoID},
	}
}

// TestExtractMetaclassRowsTargetStaysInsideTheClassFile proves a metaclass
// reference resolves only to a class in the referencing file's own
// repository and path (#7642): a metaclass defined solely in a same-named
// file elsewhere leaves the edge unresolved instead of binding across files.
func TestExtractMetaclassRowsTargetStaysInsideTheClassFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pathA string
		pathB string
	}{
		{name: "same bare name, different relative path", pathA: "lib/models.py", pathB: "app/models.py"},
		{name: "same relative path", pathA: "src/models.py", pathB: "src/models.py"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			envelopes := []facts.Envelope{
				metaclassScopeRepoEnvelope("repo-a"),
				metaclassScopeRepoEnvelope("repo-b"),
				metaclassScopeFileEnvelope("repo-a", "/repos/repo-a/"+tt.pathA, tt.pathA,
					map[string]any{"name": "Meta", "uid": "repo-a:Meta", "line_number": 1},
				),
				metaclassScopeFileEnvelope("repo-b", "/repos/repo-b/"+tt.pathB, tt.pathB,
					map[string]any{"name": "Widget", "uid": "repo-b:Widget", "line_number": 4, "metaclass": "Meta"},
				),
			}

			_, rows := ExtractMetaclassRows(envelopes)

			for _, row := range rows {
				if row["target_entity_id"] == "repo-a:Meta" {
					t.Fatalf("metaclass target bound across files: %v; same-file metaclass must stay in the class file", row)
				}
			}
			if len(rows) != 0 {
				t.Fatalf("len(rows) = %d, want 0: %v", len(rows), rows)
			}
		})
	}
}

// TestExtractMetaclassRowsSourceStaysInsideTheClassFile proves the
// uid-less class fallback resolves only within the class's own repository
// and path (#7642): a same-named class defined solely in a same-named file
// elsewhere must not become the edge source.
func TestExtractMetaclassRowsSourceStaysInsideTheClassFile(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		metaclassScopeRepoEnvelope("repo-a"),
		metaclassScopeRepoEnvelope("repo-b"),
		metaclassScopeFileEnvelope("repo-a", "/repos/repo-a/lib/models.py", "lib/models.py",
			map[string]any{"name": "Widget", "uid": "repo-a:Widget", "line_number": 1},
		),
		metaclassScopeFileEnvelope("repo-b", "/repos/repo-b/app/models.py", "app/models.py",
			map[string]any{"name": "Widget", "line_number": 4, "metaclass": "Meta"},
			map[string]any{"name": "Meta", "uid": "repo-b:Meta", "line_number": 1},
		),
	}

	_, rows := ExtractMetaclassRows(envelopes)

	for _, row := range rows {
		if row["source_entity_id"] == "repo-a:Widget" {
			t.Fatalf("uid-less class source bound across files: %v; same-file class lookup must stay in the class file", row)
		}
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0: %v", len(rows), rows)
	}
}

// TestExtractMetaclassRowsKeepsOwnFileSourceAndTarget is the positive
// control: uid-less source and metaclass target defined in the class's own
// file still resolve.
func TestExtractMetaclassRowsKeepsOwnFileSourceAndTarget(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		metaclassScopeRepoEnvelope("repo-a"),
		metaclassScopeRepoEnvelope("repo-b"),
		metaclassScopeFileEnvelope("repo-a", "/repos/repo-a/lib/models.py", "lib/models.py",
			map[string]any{"name": "Widget", "uid": "repo-a:Widget", "line_number": 1},
			map[string]any{"name": "Meta", "uid": "repo-a:Meta", "line_number": 8},
		),
		metaclassScopeFileEnvelope("repo-b", "/repos/repo-b/app/models.py", "app/models.py",
			map[string]any{"name": "Widget", "uid": "repo-b:Widget", "line_number": 1},
			map[string]any{"name": "Widget", "line_number": 4, "metaclass": "Meta"},
			map[string]any{"name": "Meta", "uid": "repo-b:Meta", "line_number": 8},
		),
	}

	_, rows := ExtractMetaclassRows(envelopes)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %v", len(rows), rows)
	}
	if got := rows[0]["source_entity_id"]; got != "repo-b:Widget" {
		t.Fatalf("source_entity_id = %v, want repo-b:Widget", got)
	}
	if got := rows[0]["target_entity_id"]; got != "repo-b:Meta" {
		t.Fatalf("target_entity_id = %v, want repo-b:Meta", got)
	}
}
