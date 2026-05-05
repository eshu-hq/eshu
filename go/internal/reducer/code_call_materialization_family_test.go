package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsSkipsQualifiedPHPCallsWithoutReceiverType(t *testing.T) {
	t.Parallel()

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
				"relative_path": "service.php",
				"parsed_file_data": map[string]any{
					"path": "service.php",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 3,
							"end_line":    6,
							"uid":         "content-entity:php-run",
						},
						map[string]any{
							"name":        "info",
							"line_number": 8,
							"end_line":    10,
							"uid":         "content-entity:php-info",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$service.info",
							"inferred_obj_type": nil,
							"line_number":       4,
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for receiver-qualified PHP call without inferred type", len(rows))
	}
}

func TestExtractCodeCallRowsResolvesPHPThisPropertyCallsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

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
				"relative_path": "service.php",
				"parsed_file_data": map[string]any{
					"path": "service.php",
					"functions": []any{
						map[string]any{
							"name":          "info",
							"class_context": "Service",
							"line_number":   2,
							"end_line":      4,
							"uid":           "content-entity:php-service-info",
						},
						map[string]any{
							"name":          "run",
							"class_context": "Config",
							"line_number":   8,
							"end_line":      10,
							"uid":           "content-entity:php-config-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$this->service.info",
							"inferred_obj_type": "Service",
							"line_number":       9,
							"lang":              "php",
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

func TestExtractCodeCallRowsResolvesPHPPropertyChainAliasCallsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

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
				"relative_path": "config.php",
				"parsed_file_data": map[string]any{
					"path": "config.php",
					"functions": []any{
						map[string]any{
							"name":          "info",
							"class_context": "Logger",
							"line_number":   2,
							"end_line":      4,
							"uid":           "content-entity:php-logger-info",
						},
						map[string]any{
							"name":          "run",
							"class_context": "Config",
							"line_number":   10,
							"end_line":      14,
							"uid":           "content-entity:php-config-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$logger.info",
							"inferred_obj_type": "Logger",
							"line_number":       13,
							"lang":              "php",
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

func TestExtractCodeCallRowsResolvesPHPMethodReturnPropertyChainCallsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

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
				"relative_path": "factory.php",
				"parsed_file_data": map[string]any{
					"path": "factory.php",
					"functions": []any{
						map[string]any{
							"name":          "info",
							"class_context": "Logger",
							"line_number":   2,
							"end_line":      4,
							"uid":           "content-entity:php-logger-info",
						},
						map[string]any{
							"name":          "run",
							"class_context": "Config",
							"line_number":   10,
							"end_line":      14,
							"uid":           "content-entity:php-config-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$logger.info",
							"inferred_obj_type": "Logger",
							"line_number":       13,
							"lang":              "php",
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

func TestExtractCodeCallRowsResolvesPHPNullsafeReceiverChainsUsingTypedPropertyInference(t *testing.T) {
	t.Parallel()

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
				"relative_path": "session.php",
				"parsed_file_data": map[string]any{
					"path": "session.php",
					"functions": []any{
						map[string]any{
							"name":          "info",
							"class_context": "Service",
							"line_number":   2,
							"end_line":      4,
							"uid":           "content-entity:php-service-info",
						},
						map[string]any{
							"name":          "run",
							"class_context": "Config",
							"line_number":   10,
							"end_line":      14,
							"uid":           "content-entity:php-config-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "$session->service.info",
							"inferred_obj_type": "Service",
							"line_number":       13,
							"lang":              "php",
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

func TestExtractCodeCallRowsDisambiguatesSwiftCallsUsingInferredReceiverType(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-swift",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-swift",
				"relative_path": "worker.swift",
				"parsed_file_data": map[string]any{
					"path": "worker.swift",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 10,
							"end_line":    12,
							"uid":         "content-entity:swift-run",
						},
						map[string]any{
							"name":          "info",
							"class_context": "Logger",
							"line_number":   2,
							"end_line":      4,
							"uid":           "content-entity:swift-logger-info",
						},
						map[string]any{
							"name":          "info",
							"class_context": "Queue",
							"line_number":   6,
							"end_line":      8,
							"uid":           "content-entity:swift-queue-info",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "info",
							"full_name":         "logger.info",
							"inferred_obj_type": "Logger",
							"line_number":       11,
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:swift-logger-info"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsDisambiguatesRubyModuleScopedCallsUsingContext(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-ruby",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-ruby",
				"relative_path": "basic.rb",
				"parsed_file_data": map[string]any{
					"path": "basic.rb",
					"functions": []any{
						map[string]any{
							"name":         "greet",
							"context":      "Comprehensive",
							"context_type": "module",
							"line_number":  2,
							"end_line":     4,
							"uid":          "content-entity:ruby-module-greet",
						},
						map[string]any{
							"name":          "greet",
							"class_context": "Application",
							"context":       "Application",
							"context_type":  "class",
							"line_number":   7,
							"end_line":      9,
							"uid":           "content-entity:ruby-class-greet",
						},
						map[string]any{
							"name":         "run",
							"context":      "Comprehensive",
							"context_type": "module",
							"line_number":  11,
							"end_line":     13,
							"uid":          "content-entity:ruby-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":         "greet",
							"full_name":    "greet",
							"context":      "Comprehensive",
							"context_type": "module",
							"line_number":  12,
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:ruby-module-greet"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}

func TestExtractCodeCallRowsLimitsElixirToExactQualifiedMatches(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-elixir",
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-elixir",
				"relative_path": "worker.ex",
				"parsed_file_data": map[string]any{
					"path": "worker.ex",
					"functions": []any{
						map[string]any{
							"name":          "greet",
							"class_context": "Demo.Basic",
							"line_number":   2,
							"end_line":      4,
							"uid":           "content-entity:elixir-basic-greet",
						},
						map[string]any{
							"name":          "greet",
							"class_context": "Demo.Other",
							"line_number":   6,
							"end_line":      8,
							"uid":           "content-entity:elixir-other-greet",
						},
						map[string]any{
							"name":          "run",
							"class_context": "Demo.Worker",
							"line_number":   10,
							"end_line":      14,
							"uid":           "content-entity:elixir-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "greet",
							"full_name":         "Demo.Basic.greet",
							"inferred_obj_type": "Demo.Basic",
							"line_number":       11,
						},
						map[string]any{
							"name":              "greet",
							"full_name":         "Basic.greet",
							"inferred_obj_type": "Basic",
							"line_number":       12,
						},
						map[string]any{
							"name":        "greet",
							"full_name":   "greet",
							"line_number": 13,
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
	if got, want := rows[0]["callee_entity_id"], "content-entity:elixir-basic-greet"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
}
