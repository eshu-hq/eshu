package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesPHPStaticPropertyReceiverChainsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "registry.php")
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
				"relative_path": "registry.php",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":          "boot",
							"class_context": "Registry",
							"line_number":   3,
							"end_line":      5,
							"uid":           "content-entity:php-registry-boot",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "self::$service.info",
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

func TestExtractCodeCallRowsResolvesPHPParentAndStaticPropertyReceiverChainsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "registry.php")
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
				"relative_path": "registry.php",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":          "boot",
							"class_context": "ChildRegistry",
							"line_number":   10,
							"end_line":      13,
							"uid":           "content-entity:php-registry-boot",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "parent::$service.info",
							"inferred_obj_type": "Service",
							"line_number":       11,
							"lang":              "php",
						},
						map[string]any{
							"name":              "info",
							"full_name":         "static::$service.info",
							"inferred_obj_type": "Service",
							"line_number":       12,
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
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2; rows=%#v", len(rows), rows)
	}

	want := map[string]string{
		"parent::$service.info": "content-entity:php-service-info",
		"static::$service.info": "content-entity:php-service-info",
	}
	for _, row := range rows {
		fullName, _ := row["full_name"].(string)
		if calleeID, ok := want[fullName]; ok {
			if got := row["callee_entity_id"]; got != calleeID {
				t.Fatalf("callee_entity_id = %#v, want %#v for %#v", got, calleeID, fullName)
			}
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expected static-property rows: %#v; rows=%#v", want, rows)
	}
}

func TestExtractCodeCallRowsResolvesPHPDeepStaticPropertyReceiverChainsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "registry.php")
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
				"relative_path": "registry.php",
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{
							"name":          "boot",
							"class_context": "ChildRegistry",
							"line_number":   10,
							"end_line":      14,
							"uid":           "content-entity:php-registry-boot",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "self::$factory->createService().info",
							"inferred_obj_type": "Service",
							"line_number":       11,
							"lang":              "php",
						},
						map[string]any{
							"name":              "info",
							"full_name":         "parent::$factory->createService().info",
							"inferred_obj_type": "Service",
							"line_number":       12,
							"lang":              "php",
						},
						map[string]any{
							"name":              "info",
							"full_name":         "static::$factory->createService().info",
							"inferred_obj_type": "Service",
							"line_number":       13,
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
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3; rows=%#v", len(rows), rows)
	}

	want := map[string]string{
		"self::$factory->createService().info":   "content-entity:php-service-info",
		"parent::$factory->createService().info": "content-entity:php-service-info",
		"static::$factory->createService().info": "content-entity:php-service-info",
	}
	for _, row := range rows {
		fullName, _ := row["full_name"].(string)
		if calleeID, ok := want[fullName]; ok {
			if got := row["callee_entity_id"]; got != calleeID {
				t.Fatalf("callee_entity_id = %#v, want %#v for %#v", got, calleeID, fullName)
			}
			delete(want, fullName)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expected deep static-property rows: %#v; rows=%#v", want, rows)
	}
}
