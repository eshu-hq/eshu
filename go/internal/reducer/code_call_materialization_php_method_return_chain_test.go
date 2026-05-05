package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesPHPMethodReturnCallChainReceiverCallsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.php")
	factoryPath := filepath.Join(repoRoot, "factory.php")
	servicePath := filepath.Join(repoRoot, "service.php")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-php",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "app.php",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:php-run",
						},
						map[string]any{
							"name":        "getFactory",
							"line_number": 7,
							"end_line":    9,
							"uid":         "content-entity:php-get-factory",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$this->factory.createService().info",
							"inferred_obj_type": "Service",
							"line_number":       4,
							"lang":              "php",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "factory.php",
				"parsed_file_data": map[string]any{
					"path": factoryPath,
					"functions": []any{
						map[string]any{
							"name":          "createService",
							"class_context": "Factory",
							"line_number":   1,
							"end_line":      3,
							"uid":           "content-entity:php-factory-create-service",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "service.php",
				"parsed_file_data": map[string]any{
					"path": servicePath,
					"functions": []any{
						map[string]any{
							"name":          "info",
							"class_context": "Service",
							"line_number":   1,
							"end_line":      2,
							"uid":           "content-entity:php-service-info",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:php-service-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesPHPMethodReturnPropertyDereferenceReceiverCallsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.php")
	factoryPath := filepath.Join(repoRoot, "factory.php")
	loggerPath := filepath.Join(repoRoot, "logger.php")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-php",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "app.php",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:php-run",
						},
						map[string]any{
							"name":        "createFactory",
							"line_number": 7,
							"end_line":    9,
							"uid":         "content-entity:php-create-factory",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$this->factory->createService().logger.info",
							"inferred_obj_type": "Logger",
							"line_number":       4,
							"lang":              "php",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "factory.php",
				"parsed_file_data": map[string]any{
					"path": factoryPath,
					"functions": []any{
						map[string]any{
							"name":          "createService",
							"class_context": "Factory",
							"line_number":   1,
							"end_line":      3,
							"uid":           "content-entity:php-factory-create-service",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "service.php",
				"parsed_file_data": map[string]any{
					"path": filepath.Join(repoRoot, "service.php"),
					"classes": []any{
						map[string]any{
							"name":        "Service",
							"line_number": 1,
							"end_line":    5,
							"uid":         "content-entity:php-service",
						},
					},
					"variables": []any{
						map[string]any{
							"name":        "$logger",
							"type":        "Logger",
							"line_number": 2,
							"end_line":    2,
							"uid":         "content-entity:php-service-logger",
						},
					},
					"functions": []any{},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "logger.php",
				"parsed_file_data": map[string]any{
					"path": loggerPath,
					"functions": []any{
						map[string]any{
							"name":          "info",
							"class_context": "Logger",
							"line_number":   1,
							"end_line":      2,
							"uid":           "content-entity:php-logger-info",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:php-logger-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsResolvesPHPMethodReturnPropertyDereferenceReceiverCallsWithNamespacedTypedPropertyInference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "app.php")
	factoryPath := filepath.Join(repoRoot, "factory.php")
	loggerPath := filepath.Join(repoRoot, "logger.php")

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-php",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "app.php",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    5,
							"uid":         "content-entity:php-run",
						},
						map[string]any{
							"name":        "createFactory",
							"line_number": 7,
							"end_line":    9,
							"uid":         "content-entity:php-create-factory",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$this->factory->createService().logger.info",
							"inferred_obj_type": "Demo\\Logger",
							"line_number":       4,
							"lang":              "php",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "factory.php",
				"parsed_file_data": map[string]any{
					"path": factoryPath,
					"functions": []any{
						map[string]any{
							"name":          "createService",
							"class_context": "Factory",
							"line_number":   1,
							"end_line":      3,
							"uid":           "content-entity:php-factory-create-service",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-php",
				"relative_path": "logger.php",
				"parsed_file_data": map[string]any{
					"path": loggerPath,
					"functions": []any{
						map[string]any{
							"name":          "info",
							"class_context": "Logger",
							"line_number":   1,
							"end_line":      2,
							"uid":           "content-entity:php-logger-info",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["callee_entity_id"], "content-entity:php-logger-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}
