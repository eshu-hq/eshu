package reducer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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

func TestPythonMetaclassMaterializationHandlerWritesCanonicalEdges(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":       "repo-python",
					"source_run_id": "run-python",
					"graph_id":      "repo-python",
					"graph_kind":    "repository",
					"name":          "repo-python",
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-python",
					"relative_path": "models.py",
					"parsed_file_data": map[string]any{
						"path": "models.py",
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
		},
	}
	writer := &recordingCodeCallIntentWriter{}
	handler := CodeCallMaterializationHandler{
		FactLoader:   loader,
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-python-metaclass-1",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainCodeCallMaterialization,
		Cause:           "repository snapshot emitted code-call materialization follow-up",
		EntityKeys:      []string{"repo:repo-python"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Now().UTC(),
		AvailableAt:     time.Now().UTC(),
		Status:          IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := loader.calls, 1; got != want {
		t.Fatalf("loader.calls = %d, want %d", got, want)
	}
	if got, want := len(writer.rows), 2; got != want {
		t.Fatalf("len(writer.rows) = %d, want %d", got, want)
	}
	if got, want := writer.rows[0].Payload["action"], "refresh"; got != want {
		t.Fatalf("refresh row action = %#v, want %#v", got, want)
	}
	if got, want := writer.rows[1].Payload["source_entity_id"], "content-entity:logged"; got != want {
		t.Fatalf("write row source_entity_id = %#v, want %#v", got, want)
	}
	if got, want := writer.rows[1].Payload["target_entity_id"], "content-entity:meta"; got != want {
		t.Fatalf("write row target_entity_id = %#v, want %#v", got, want)
	}
	if got, want := writer.rows[1].Payload["relationship_type"], "USES_METACLASS"; got != want {
		t.Fatalf("write row relationship_type = %#v, want %#v", got, want)
	}
	if got, want := writer.rows[1].Payload["evidence_source"], pythonMetaclassEvidenceSource; got != want {
		t.Fatalf("write row evidence_source = %#v, want %#v", got, want)
	}
	if got, want := result.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("result.Status = %q, want %q", got, want)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("result.CanonicalWrites = %d, want %d", got, want)
	}
}
