package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSemanticEntityWriterWritesAnnotationTypedefTypeAliasComponentAndFunctionNodes(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "annotation-1",
				EntityType:   "Annotation",
				EntityName:   "Logged",
				FilePath:     "/repo/src/Logged.java",
				RelativePath: "src/Logged.java",
				Language:     "java",
				StartLine:    12,
				EndLine:      12,
				Metadata: map[string]any{
					"kind":        "applied",
					"target_kind": "method_declaration",
				},
			},
			{
				RepoID:       "repo-1",
				EntityID:     "typedef-1",
				EntityType:   "Typedef",
				EntityName:   "my_int",
				FilePath:     "/repo/src/types.h",
				RelativePath: "src/types.h",
				Language:     "c",
				StartLine:    3,
				EndLine:      3,
				Metadata: map[string]any{
					"type": "int",
				},
			},
			{
				RepoID:       "repo-1",
				EntityID:     "alias-1",
				EntityType:   "TypeAlias",
				EntityName:   "UserID",
				FilePath:     "/repo/src/types.ts",
				RelativePath: "src/types.ts",
				Language:     "typescript",
				StartLine:    8,
				EndLine:      8,
				Metadata: map[string]any{
					"type_alias_kind": "mapped_type",
					"type_parameters": []any{"T"},
				},
			},
			{
				RepoID:       "repo-1",
				EntityID:     "component-1",
				EntityType:   "Component",
				EntityName:   "Button",
				FilePath:     "/repo/src/Button.tsx",
				RelativePath: "src/Button.tsx",
				Language:     "tsx",
				StartLine:    4,
				EndLine:      4,
				Metadata: map[string]any{
					"framework":                "react",
					"jsx_fragment_shorthand":   true,
					"component_type_assertion": "ComponentType",
				},
			},
			{
				RepoID:       "repo-1",
				EntityID:     "function-1",
				EntityType:   "Function",
				EntityName:   "getTab",
				FilePath:     "/repo/src/app.js",
				RelativePath: "src/app.js",
				Language:     "javascript",
				StartLine:    10,
				EndLine:      24,
				Metadata: map[string]any{
					"docstring":   "Returns the active tab.",
					"method_kind": "getter",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 5; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 6; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if executor.calls[0].Operation != OperationCanonicalRetract {
		t.Fatalf("call[0].Operation = %q, want %q", executor.calls[0].Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(executor.calls[0].Cypher, "DETACH DELETE n") {
		t.Fatalf("call[0].Cypher missing DETACH DELETE: %s", executor.calls[0].Cypher)
	}

	annotationRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := len(annotationRows), 1; got != want {
		t.Fatalf("annotation row count = %d, want %d", got, want)
	}
	if got, want := annotationRows[0]["kind"], "applied"; got != want {
		t.Fatalf("annotation kind = %#v, want %#v", got, want)
	}
	if got, want := annotationRows[0]["target_kind"], "method_declaration"; got != want {
		t.Fatalf("annotation target_kind = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "MERGE (n:Annotation {uid: row.entity_id})") {
		t.Fatalf("annotation cypher missing Annotation merge: %s", executor.calls[1].Cypher)
	}

	typedefRows := executor.calls[2].Parameters["rows"].([]map[string]any)
	if got, want := len(typedefRows), 1; got != want {
		t.Fatalf("typedef row count = %d, want %d", got, want)
	}
	if got, want := typedefRows[0]["type"], "int"; got != want {
		t.Fatalf("typedef type = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[2].Cypher, "MERGE (n:Typedef {uid: row.entity_id})") {
		t.Fatalf("typedef cypher missing Typedef merge: %s", executor.calls[2].Cypher)
	}

	typeAliasRows := executor.calls[3].Parameters["rows"].([]map[string]any)
	if got, want := len(typeAliasRows), 1; got != want {
		t.Fatalf("type alias row count = %d, want %d", got, want)
	}
	if got, want := typeAliasRows[0]["type_alias_kind"], "mapped_type"; got != want {
		t.Fatalf("type alias kind = %#v, want %#v", got, want)
	}
	typeParameters, ok := typeAliasRows[0]["type_parameters"].([]string)
	if !ok {
		t.Fatalf("type alias type_parameters type = %T, want []string", typeAliasRows[0]["type_parameters"])
	}
	if got, want := len(typeParameters), 1; got != want || typeParameters[0] != "T" {
		t.Fatalf("type alias type_parameters = %#v, want [T]", typeParameters)
	}
	if !strings.Contains(executor.calls[3].Cypher, "MERGE (n:TypeAlias {uid: row.entity_id})") {
		t.Fatalf("type alias cypher missing TypeAlias merge: %s", executor.calls[3].Cypher)
	}

	componentRows := executor.calls[4].Parameters["rows"].([]map[string]any)
	if got, want := len(componentRows), 1; got != want {
		t.Fatalf("component row count = %d, want %d", got, want)
	}
	if got, want := componentRows[0]["framework"], "react"; got != want {
		t.Fatalf("component framework = %#v, want %#v", got, want)
	}
	if got, want := componentRows[0]["jsx_fragment_shorthand"], true; got != want {
		t.Fatalf("component jsx_fragment_shorthand = %#v, want %#v", got, want)
	}
	if got, want := componentRows[0]["component_type_assertion"], "ComponentType"; got != want {
		t.Fatalf("component component_type_assertion = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[4].Cypher, "MERGE (n:Component {uid: row.entity_id})") {
		t.Fatalf("component cypher missing Component merge: %s", executor.calls[4].Cypher)
	}

	functionRows := executor.calls[5].Parameters["rows"].([]map[string]any)
	if got, want := len(functionRows), 1; got != want {
		t.Fatalf("function row count = %d, want %d", got, want)
	}
	if got, want := functionRows[0]["docstring"], "Returns the active tab."; got != want {
		t.Fatalf("function docstring = %#v, want %#v", got, want)
	}
	if got, want := functionRows[0]["method_kind"], "getter"; got != want {
		t.Fatalf("function method_kind = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[5].Cypher, "MERGE (n:Function {uid: row.entity_id})") {
		t.Fatalf("function cypher missing Function merge: %s", executor.calls[5].Cypher)
	}
}

func TestSemanticEntityWriterWritesPythonFunctionSemanticMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "function-1",
				EntityType:   "Function",
				EntityName:   "handler",
				FilePath:     "/repo/src/app.py",
				RelativePath: "src/app.py",
				Language:     "python",
				StartLine:    10,
				EndLine:      20,
				Metadata: map[string]any{
					"decorators":            []any{"@route"},
					"async":                 true,
					"type_annotation_count": 2,
					"type_annotation_kinds": []string{"parameter", "return"},
				},
			},
			{
				RepoID:       "repo-1",
				EntityID:     "function-2",
				EntityType:   "Function",
				EntityName:   "double",
				FilePath:     "/repo/src/lambda.py",
				RelativePath: "src/lambda.py",
				Language:     "python",
				StartLine:    30,
				EndLine:      30,
				Metadata: map[string]any{
					"semantic_kind": "lambda",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	functionRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := len(functionRows), 2; got != want {
		t.Fatalf("function row count = %d, want %d", got, want)
	}

	decorated := functionRows[0]
	if got, want := decorated["entity_id"], "function-1"; got != want {
		t.Fatalf("decorated entity_id = %#v, want %#v", got, want)
	}
	if got, want := decorated["async"], true; got != want {
		t.Fatalf("decorated async = %#v, want %#v", got, want)
	}
	if got, want := decorated["type_annotation_count"], 2; got != want {
		t.Fatalf("decorated type_annotation_count = %#v, want %#v", got, want)
	}
	kinds, ok := decorated["type_annotation_kinds"].([]string)
	if !ok {
		t.Fatalf("decorated type_annotation_kinds type = %T, want []string", decorated["type_annotation_kinds"])
	}
	if got, want := len(kinds), 2; got != want || kinds[0] != "parameter" || kinds[1] != "return" {
		t.Fatalf("decorated type_annotation_kinds = %#v, want [parameter return]", kinds)
	}
	decorators, ok := decorated["decorators"].([]string)
	if !ok {
		t.Fatalf("decorated decorators type = %T, want []string", decorated["decorators"])
	}
	if got, want := len(decorators), 1; got != want || decorators[0] != "@route" {
		t.Fatalf("decorated decorators = %#v, want [@route]", decorators)
	}
	if decorated["semantic_kind"] != nil {
		t.Fatalf("decorated semantic_kind = %#v, want nil", decorated["semantic_kind"])
	}

	lambda := functionRows[1]
	if got, want := lambda["entity_id"], "function-2"; got != want {
		t.Fatalf("lambda entity_id = %#v, want %#v", got, want)
	}
	if got, want := lambda["semantic_kind"], "lambda"; got != want {
		t.Fatalf("lambda semantic_kind = %#v, want %#v", got, want)
	}
	if got, ok := lambda["decorators"].([]string); ok && len(got) != 0 {
		t.Fatalf("lambda decorators = %#v, want empty or absent", got)
	}
	if cypher := executor.calls[1].Cypher; !strings.Contains(cypher, "n.decorators = row.decorators") || !strings.Contains(cypher, "n.async = row.async") || !strings.Contains(cypher, "n.semantic_kind = coalesce(row.semantic_kind, row.entity_type)") {
		t.Fatalf("function cypher missing python metadata assignments: %s", cypher)
	}
}

func TestSemanticEntityWriterWritesGoFunctionClassContextMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "function-go-1",
				EntityType:   "Function",
				EntityName:   "handleRelationships",
				FilePath:     "/repo/go/internal/query/code_relationships.go",
				RelativePath: "go/internal/query/code_relationships.go",
				Language:     "go",
				StartLine:    22,
				EndLine:      178,
				Metadata: map[string]any{
					"docstring":     "handleRelationships returns incoming and outgoing relationships for an entity.",
					"class_context": "CodeHandler",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	functionRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := len(functionRows), 1; got != want {
		t.Fatalf("function row count = %d, want %d", got, want)
	}
	if got, want := functionRows[0]["class_context"], "CodeHandler"; got != want {
		t.Fatalf("function class_context = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "n.class_context = row.class_context") {
		t.Fatalf("function cypher missing class_context projection: %s", executor.calls[1].Cypher)
	}
}

func TestSemanticEntityWriterWritesKotlinSecondaryConstructorSemanticMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "function-ctor-1",
				EntityType:   "Function",
				EntityName:   "constructor",
				FilePath:     "/repo/src/Widget.kt",
				RelativePath: "src/Widget.kt",
				Language:     "kotlin",
				StartLine:    14,
				EndLine:      14,
				Metadata: map[string]any{
					"constructor_kind": "secondary",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	functionRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := len(functionRows), 1; got != want {
		t.Fatalf("function row count = %d, want %d", got, want)
	}
	if got, want := functionRows[0]["constructor_kind"], "secondary"; got != want {
		t.Fatalf("function constructor_kind = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "n.constructor_kind = row.constructor_kind") {
		t.Fatalf("function cypher missing constructor_kind projection: %s", executor.calls[1].Cypher)
	}
}

func TestSemanticEntityWriterWritesElixirGuardFunctionMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "function-guard-1",
				EntityType:   "Function",
				EntityName:   "is_even",
				FilePath:     "/repo/lib/demo/macros.ex",
				RelativePath: "lib/demo/macros.ex",
				Language:     "elixir",
				StartLine:    10,
				EndLine:      10,
				Metadata: map[string]any{
					"semantic_kind": "guard",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	functionRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := len(functionRows), 1; got != want {
		t.Fatalf("function row count = %d, want %d", got, want)
	}
	if got, want := functionRows[0]["semantic_kind"], "guard"; got != want {
		t.Fatalf("function semantic_kind = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "MERGE (n:Function {uid: row.entity_id})") {
		t.Fatalf("function cypher missing Function merge: %s", executor.calls[1].Cypher)
	}
}
