package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractInheritanceRowsMaterializesImplementedInterfacesSeparately(t *testing.T) {
	t.Parallel()

	_, rows := ExtractInheritanceRows([]facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "class-service",
				"entity_type": "Class",
				"entity_name": "Service",
				"entity_metadata": map[string]any{
					"bases":                  []any{"BaseService", "RunnableService"},
					"implemented_interfaces": []any{"RunnableService"},
				},
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "class-base",
				"entity_type": "Class",
				"entity_name": "BaseService",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "iface-runnable",
				"entity_type": "Interface",
				"entity_name": "RunnableService",
			},
		},
	})

	assertRelationshipRow(t, rows, "class-service", "class-base", "INHERITS")
	assertRelationshipRow(t, rows, "class-service", "iface-runnable", "IMPLEMENTS")
	assertNoRelationshipRow(t, rows, "class-service", "iface-runnable", "INHERITS")
}

func TestExtractCodeCallRowsMaterializesConstructorInstantiations(t *testing.T) {
	t.Parallel()

	repoEnvelope := facts.Envelope{
		FactKind: "repository",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":       "repo-1",
			"source_run_id": "run-1",
		},
	}
	fileEnvelope := facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-1",
			"relative_path": "src/service.ts",
			"parsed_file_data": map[string]any{
				"path": "src/service.ts",
				"classes": []map[string]any{
					{
						"uid":         "class-service",
						"name":        "Service",
						"line_number": 1,
						"end_line":    5,
					},
				},
				"functions": []map[string]any{
					{
						"uid":         "function-build",
						"name":        "build",
						"line_number": 7,
						"end_line":    9,
					},
				},
				"function_calls": []map[string]any{
					{
						"name":        "Service",
						"full_name":   "Service",
						"call_kind":   "constructor_call",
						"line_number": 8,
						"lang":        "typescript",
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows([]facts.Envelope{repoEnvelope, fileEnvelope})

	assertCodeRelationshipRow(t, rows, "function-build", "class-service", "INSTANTIATES")
}

func TestExtractCodeCallRowsKeepsJavaConstructorMethodCall(t *testing.T) {
	t.Parallel()

	repoEnvelope := facts.Envelope{
		FactKind: "repository",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":       "repo-1",
			"source_run_id": "run-1",
		},
	}
	fileEnvelope := facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-1",
			"relative_path": "src/Service.java",
			"parsed_file_data": map[string]any{
				"path": "src/Service.java",
				"classes": []map[string]any{
					{
						"uid":         "class-service",
						"name":        "Service",
						"line_number": 1,
						"end_line":    10,
					},
				},
				"functions": []map[string]any{
					{
						"uid":           "constructor-service",
						"name":          "Service",
						"class_context": "Service",
						"line_number":   2,
						"end_line":      4,
					},
					{
						"uid":         "function-build",
						"name":        "build",
						"line_number": 6,
						"end_line":    8,
					},
				},
				"function_calls": []map[string]any{
					{
						"name":        "Service",
						"full_name":   "Service",
						"call_kind":   "constructor_call",
						"line_number": 7,
						"lang":        "java",
					},
				},
			},
		},
	}

	_, rows := ExtractCodeCallRows([]facts.Envelope{repoEnvelope, fileEnvelope})

	assertCodeRelationshipRow(t, rows, "function-build", "class-service", "INSTANTIATES")
	assertCodeRelationshipRow(t, rows, "function-build", "constructor-service", "CALLS")
}

func assertRelationshipRow(
	t *testing.T,
	rows []map[string]any,
	childID string,
	parentID string,
	relationshipType string,
) {
	t.Helper()
	for _, row := range rows {
		if anyToString(row["child_entity_id"]) == childID &&
			anyToString(row["parent_entity_id"]) == parentID &&
			anyToString(row["relationship_type"]) == relationshipType {
			return
		}
	}
	t.Fatalf("missing %s relationship %s -> %s in %#v", relationshipType, childID, parentID, rows)
}

func assertNoRelationshipRow(
	t *testing.T,
	rows []map[string]any,
	childID string,
	parentID string,
	relationshipType string,
) {
	t.Helper()
	for _, row := range rows {
		if anyToString(row["child_entity_id"]) == childID &&
			anyToString(row["parent_entity_id"]) == parentID &&
			anyToString(row["relationship_type"]) == relationshipType {
			t.Fatalf("unexpected %s relationship %s -> %s in %#v", relationshipType, childID, parentID, rows)
		}
	}
}

func assertCodeRelationshipRow(
	t *testing.T,
	rows []map[string]any,
	callerID string,
	calleeID string,
	relationshipType string,
) {
	t.Helper()
	for _, row := range rows {
		if anyToString(row["caller_entity_id"]) == callerID &&
			anyToString(row["callee_entity_id"]) == calleeID &&
			anyToString(row["relationship_type"]) == relationshipType {
			return
		}
	}
	t.Fatalf("missing %s relationship %s -> %s in %#v", relationshipType, callerID, calleeID, rows)
}
