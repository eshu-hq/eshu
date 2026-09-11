// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractPythonMetaclassRowsBuildsCanonicalEntityPairs(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "models.py")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-python",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "models.py",
				"parsed_file_data": map[string]any{
					"path": filePath,
					"classes": []any{
						map[string]any{
							"name":        "MetaLogger",
							"line_number": 1,
							"uid":         "content-entity:meta",
						},
						map[string]any{
							"name":        "Logged",
							"line_number": 4,
							"uid":         "content-entity:logged",
							"metaclass":   "MetaLogger",
						},
					},
				},
			},
		},
	}

	repoIDs, rows := ExtractPythonMetaclassRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-python" {
		t.Fatalf("repoIDs = %v, want [repo-python]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["source_entity_id"], "content-entity:logged"; got != want {
		t.Fatalf("source_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:meta"; got != want {
		t.Fatalf("target_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractPythonMetaclassRowsResolvesImportedMetaclasses(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "models.py")
	metaclassPath := filepath.Join(repoRoot, "meta.py")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-python",
				"imports_map": map[string][]string{
					"MetaLogger": {metaclassPath},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "models.py",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"imports": []any{
						map[string]any{
							"name":        "MetaLogger",
							"source":      "./meta",
							"lang":        "python",
							"import_type": "from",
						},
					},
					"classes": []any{
						map[string]any{
							"name":        "Logged",
							"line_number": 4,
							"uid":         "content-entity:logged",
							"metaclass":   "MetaLogger",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-python",
				"relative_path": "meta.py",
				"parsed_file_data": map[string]any{
					"path": metaclassPath,
					"classes": []any{
						map[string]any{
							"name":        "MetaLogger",
							"line_number": 1,
							"uid":         "content-entity:meta",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractPythonMetaclassRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:meta"; got != want {
		t.Fatalf("target_entity_id = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["target_file"], "meta.py"; got != want {
		t.Fatalf("target_file = %#v, want %#v", got, want)
	}
}
