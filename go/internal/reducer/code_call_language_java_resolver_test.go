// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestResolveGenericCalleeUsesJavaReceiverTypeBeforeRepoUniqueName(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Worker.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Worker.java",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 1,
							"end_line":    5,
							"uid":         "content-entity:java-worker-run",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "process",
							"full_name":         "process",
							"inferred_obj_type": "Service",
							"argument_types":    []any{"Task"},
							"line_number":       3,
							"lang":              "java",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Service.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-service-process",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Other.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Other.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Other",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-other-process",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "content-entity:java-service-process"); got != codeprovenance.MethodTypeInferred {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodTypeInferred)
	}
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-worker-run", "content-entity:java-other-process")
}

func TestResolveGenericCalleeUsesJavaImportedReceiverBeforeAmbiguousRepoName(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-java",
				"imports_map": map[string][]string{
					"Service": {
						"src/main/java/com/acme/Service.java",
						"src/main/java/com/other/Service.java",
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Worker.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Worker.java",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 1,
							"end_line":    5,
							"uid":         "content-entity:java-worker-run",
						},
					},
					"imports": []any{
						map[string]any{
							"name":        "com.acme.Service",
							"alias":       "Service",
							"source":      "com.acme.Service",
							"import_type": "import",
							"lang":        "java",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "process",
							"full_name":         "service.process",
							"inferred_obj_type": "Service",
							"argument_types":    []any{"Task"},
							"argument_count":    1,
							"line_number":       3,
							"lang":              "java",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/com/acme/Service.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/com/acme/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-acme-process",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/com/other/Service.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/com/other/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-other-process",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "content-entity:java-acme-process"); got != codeprovenance.MethodImportBinding {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodImportBinding)
	}
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-worker-run", "content-entity:java-other-process")
}

func TestResolveGenericCalleeLeavesAmbiguousJavaImportedReceiverUnresolved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-java",
				"imports_map": map[string][]string{
					"Service": {
						"module-a/src/main/java/com/acme/Service.java",
						"module-b/src/main/java/com/acme/Service.java",
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Worker.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Worker.java",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 1,
							"end_line":    5,
							"uid":         "content-entity:java-worker-run",
						},
					},
					"imports": []any{
						map[string]any{
							"name":        "com.acme.Service",
							"alias":       "Service",
							"source":      "com.acme.Service",
							"import_type": "import",
							"lang":        "java",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "process",
							"full_name":         "service.process",
							"inferred_obj_type": "Service",
							"argument_types":    []any{"Task"},
							"argument_count":    1,
							"line_number":       3,
							"lang":              "java",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "module-a/src/main/java/com/acme/Service.java",
				"parsed_file_data": map[string]any{
					"path": "module-a/src/main/java/com/acme/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-module-a-process",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "module-b/src/main/java/com/acme/Service.java",
				"parsed_file_data": map[string]any{
					"path": "module-b/src/main/java/com/acme/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-module-b-process",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-worker-run", "content-entity:java-module-a-process")
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-worker-run", "content-entity:java-module-b-process")
}

func TestResolveGenericCalleeLeavesDuplicateJavaImportBindingUnresolvedBeforeMethodLookup(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-java",
				"imports_map": map[string][]string{
					"Service": {
						"module-a/src/main/java/com/acme/Service.java",
						"module-b/src/main/java/com/acme/Service.java",
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Worker.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Worker.java",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 1,
							"end_line":    5,
							"uid":         "content-entity:java-worker-run",
						},
					},
					"imports": []any{
						map[string]any{
							"name":        "com.acme.Service",
							"alias":       "Service",
							"source":      "com.acme.Service",
							"import_type": "import",
							"lang":        "java",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":              "process",
							"full_name":         "service.process",
							"inferred_obj_type": "Service",
							"argument_types":    []any{"Task"},
							"argument_count":    1,
							"line_number":       3,
							"lang":              "java",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "module-a/src/main/java/com/acme/Service.java",
				"parsed_file_data": map[string]any{
					"path": "module-a/src/main/java/com/acme/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-module-a-process",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "module-b/src/main/java/com/acme/Service.java",
				"parsed_file_data": map[string]any{
					"path": "module-b/src/main/java/com/acme/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "configure",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-module-b-configure",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-worker-run", "content-entity:java-module-a-process")
}

func TestResolveGenericCalleeDoesNotBindQualifiedJavaReceiverToConflictingImport(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-java",
				"imports_map": map[string][]string{
					"Service": {
						"src/main/java/com/acme/Service.java",
						"src/main/java/com/other/Service.java",
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/example/Worker.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/example/Worker.java",
					"functions": []any{
						map[string]any{
							"name":        "run",
							"line_number": 1,
							"end_line":    5,
							"uid":         "content-entity:java-worker-run",
						},
					},
					"imports": []any{
						map[string]any{
							"name":        "com.acme.Service",
							"alias":       "Service",
							"source":      "com.acme.Service",
							"import_type": "import",
							"lang":        "java",
						},
					},
					"function_calls": []any{
						map[string]any{
							"name":                        "process",
							"full_name":                   "service.process",
							"inferred_obj_type":           "Service",
							"inferred_obj_qualified_type": "com.other.Service",
							"argument_types":              []any{"Task"},
							"argument_count":              1,
							"line_number":                 3,
							"lang":                        "java",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/com/acme/Service.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/com/acme/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-acme-process",
						},
					},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-java",
				"relative_path": "src/main/java/com/other/Service.java",
				"parsed_file_data": map[string]any{
					"path": "src/main/java/com/other/Service.java",
					"functions": []any{
						map[string]any{
							"name":            "process",
							"class_context":   "Service",
							"parameter_types": []any{"Task"},
							"line_number":     1,
							"end_line":        3,
							"uid":             "content-entity:java-other-process",
						},
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertReducerNoCodeCallRow(t, rows, "content-entity:java-worker-run", "content-entity:java-acme-process")
}
