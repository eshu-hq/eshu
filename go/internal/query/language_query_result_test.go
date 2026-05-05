package query

import (
	"reflect"
	"testing"
)

func TestBuildLanguageResult_Entity(t *testing.T) {
	row := map[string]any{
		"entity_id":  "func:abc",
		"name":       "doStuff",
		"labels":     []any{"Function"},
		"file_path":  "src/main.go",
		"repo_id":    "repo:123",
		"repo_name":  "my-repo",
		"language":   "go",
		"start_line": int64(10),
		"end_line":   int64(20),
	}

	result := buildLanguageResult(row, "Function")
	if result["entity_id"] != "func:abc" {
		t.Errorf("entity_id = %v", result["entity_id"])
	}
	if result["name"] != "doStuff" {
		t.Errorf("name = %v", result["name"])
	}
	if result["file_path"] != "src/main.go" {
		t.Errorf("file_path = %v", result["file_path"])
	}
	if result["start_line"] != 10 {
		t.Errorf("start_line = %v", result["start_line"])
	}
}

func TestBuildLanguageResult_Repository(t *testing.T) {
	row := map[string]any{
		"id":         "repo:123",
		"name":       "my-repo",
		"local_path": "/repos/my-repo",
		"remote_url": "https://github.com/org/my-repo",
		"file_count": int64(42),
	}

	result := buildLanguageResult(row, "Repository")
	if result["id"] != "repo:123" {
		t.Errorf("id = %v", result["id"])
	}
	if result["file_count"] != 42 {
		t.Errorf("file_count = %v", result["file_count"])
	}
}

func TestBuildLanguageResult_AttachesGraphMetadataAndSemanticSummary(t *testing.T) {
	row := map[string]any{
		"entity_id":   "func:js:getTab",
		"name":        "getTab",
		"labels":      []any{"Function"},
		"file_path":   "src/app.js",
		"repo_id":     "repo:js",
		"repo_name":   "ui",
		"language":    "javascript",
		"start_line":  int64(10),
		"end_line":    int64(24),
		"docstring":   "Returns the active tab.",
		"method_kind": "getter",
	}

	result := buildLanguageResult(row, "Function")

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["docstring"], "Returns the active tab."; got != want {
		t.Fatalf("metadata[docstring] = %#v, want %#v", got, want)
	}
	if got, want := metadata["method_kind"], "getter"; got != want {
		t.Fatalf("metadata[method_kind] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Function getTab has JavaScript method kind getter and is documented as \"Returns the active tab.\"."; got != want {
		t.Fatalf("semantic_summary = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("semantic_profile type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "javascript_method"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestBuildLanguageResult_AttachesPythonGraphMetadataAndSemanticSummary(t *testing.T) {
	row := map[string]any{
		"entity_id":     "class:py:Logged",
		"name":          "Logged",
		"labels":        []any{"Class"},
		"file_path":     "src/models.py",
		"repo_id":       "repo:py",
		"repo_name":     "service",
		"language":      "python",
		"start_line":    int64(4),
		"end_line":      int64(8),
		"decorators":    []any{"@tracked"},
		"async":         false,
		"semantic_kind": "",
		"metaclass":     "MetaLogger",
	}

	result := buildLanguageResult(row, "Class")

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata type = %T, want map[string]any", result["metadata"])
	}
	decorators, ok := metadata["decorators"].([]any)
	if !ok {
		t.Fatalf("metadata[decorators] type = %T, want []any", metadata["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@tracked" {
		t.Fatalf("metadata[decorators] = %#v, want [@tracked]", decorators)
	}
	if got, want := metadata["metaclass"], "MetaLogger"; got != want {
		t.Fatalf("metadata[metaclass] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Class Logged uses decorators @tracked and uses metaclass MetaLogger."; got != want {
		t.Fatalf("semantic_summary = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := result["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("python_semantics type = %T, want map[string]any", result["python_semantics"])
	}
	pythonDecorators, ok := pythonSemantics["decorators"].([]string)
	if !ok {
		t.Fatalf("python_semantics[decorators] type = %T, want []string", pythonSemantics["decorators"])
	}
	if len(pythonDecorators) != 1 || pythonDecorators[0] != "@tracked" {
		t.Fatalf("python_semantics[decorators] = %#v, want [@tracked]", pythonDecorators)
	}
	if got, want := pythonSemantics["metaclass"], "MetaLogger"; got != want {
		t.Fatalf("python_semantics[metaclass] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["surface_kind"], "decorated_class"; got != want {
		t.Fatalf("python_semantics[surface_kind] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("semantic_profile type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "decorated_class"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["metaclass"], "MetaLogger"; got != want {
		t.Fatalf("semantic_profile[metaclass] = %#v, want %#v", got, want)
	}
}

func TestBuildLanguageResult_AttachesPythonGeneratorGraphMetadataAndSemanticSummary(t *testing.T) {
	row := map[string]any{
		"entity_id":     "func:py:create_ids",
		"name":          "create_ids",
		"labels":        []any{"Function"},
		"file_path":     "src/generators.py",
		"repo_id":       "repo:py",
		"repo_name":     "service",
		"language":      "python",
		"start_line":    int64(1),
		"end_line":      int64(3),
		"semantic_kind": "generator",
	}

	result := buildLanguageResult(row, "Function")

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["semantic_kind"], "generator"; got != want {
		t.Fatalf("metadata[semantic_kind] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Function create_ids is a generator."; got != want {
		t.Fatalf("semantic_summary = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("semantic_profile type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "generator_function"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := result["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("python_semantics type = %T, want map[string]any", result["python_semantics"])
	}
	if got, want := pythonSemantics["generator"], true; got != want {
		t.Fatalf("python_semantics[generator] = %#v, want %#v", got, want)
	}
}

func TestBuildLanguageResult_AttachesPythonTypeAnnotationGraphMetadata(t *testing.T) {
	row := map[string]any{
		"entity_id":       "type-ann:py:name",
		"name":            "name",
		"labels":          []any{"TypeAnnotation"},
		"file_path":       "src/app.py",
		"repo_id":         "repo:py",
		"repo_name":       "service",
		"language":        "python",
		"start_line":      int64(10),
		"end_line":        int64(10),
		"annotation_kind": "parameter",
		"context":         "greet",
		"type":            "str",
	}

	result := buildLanguageResult(row, "TypeAnnotation")

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["annotation_kind"], "parameter"; got != want {
		t.Fatalf("metadata[annotation_kind] = %#v, want %#v", got, want)
	}
	if got, want := metadata["context"], "greet"; got != want {
		t.Fatalf("metadata[context] = %#v, want %#v", got, want)
	}
	if got, want := metadata["type"], "str"; got != want {
		t.Fatalf("metadata[type] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "TypeAnnotation name is a parameter annotation for greet with type str."; got != want {
		t.Fatalf("semantic_summary = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("semantic_profile type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "parameter_type_annotation"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["annotation_kind"], "parameter"; got != want {
		t.Fatalf("semantic_profile[annotation_kind] = %#v, want %#v", got, want)
	}
}

func TestBuildLanguageResult_AttachesPythonTypeAnnotationProjection(t *testing.T) {
	row := map[string]any{
		"entity_id":             "func:py:greet",
		"name":                  "greet",
		"labels":                []any{"Function"},
		"file_path":             "src/app.py",
		"repo_id":               "repo:py",
		"repo_name":             "service",
		"language":              "python",
		"start_line":            int64(10),
		"end_line":              int64(24),
		"type_annotation_count": int64(2),
		"type_annotation_kinds": []any{"parameter", "return"},
	}

	result := buildLanguageResult(row, "Function")

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["type_annotation_count"], 2; got != want {
		t.Fatalf("metadata[type_annotation_count] = %#v, want %#v", got, want)
	}
	kinds, ok := metadata["type_annotation_kinds"].([]any)
	if !ok {
		t.Fatalf("metadata[type_annotation_kinds] type = %T, want []any", metadata["type_annotation_kinds"])
	}
	if len(kinds) != 2 || kinds[0] != "parameter" || kinds[1] != "return" {
		t.Fatalf("metadata[type_annotation_kinds] = %#v, want [parameter return]", kinds)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("semantic_profile type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "type_annotation"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["type_annotation_count"], 2; got != want {
		t.Fatalf("semantic_profile[type_annotation_count] = %#v, want %#v", got, want)
	}
	typeAnnotationKinds, ok := profile["type_annotation_kinds"].([]string)
	if !ok {
		t.Fatalf("semantic_profile[type_annotation_kinds] type = %T, want []string", profile["type_annotation_kinds"])
	}
	if len(typeAnnotationKinds) != 2 || typeAnnotationKinds[0] != "parameter" || typeAnnotationKinds[1] != "return" {
		t.Fatalf("semantic_profile[type_annotation_kinds] = %#v, want [parameter return]", typeAnnotationKinds)
	}
}

func TestBuildLanguageResult_AttachesCombinedPythonDecoratorAsyncAndTypeAnnotationMetadata(t *testing.T) {
	row := map[string]any{
		"entity_id":             "func:py:handler",
		"name":                  "handler",
		"labels":                []any{"Function"},
		"file_path":             "src/app.py",
		"repo_id":               "repo:py",
		"repo_name":             "service",
		"language":              "python",
		"start_line":            int64(10),
		"end_line":              int64(24),
		"decorators":            []any{"@route"},
		"async":                 true,
		"type_annotation_count": int64(2),
		"type_annotation_kinds": []any{"parameter", "return"},
	}

	result := buildLanguageResult(row, "Function")

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["decorators"], []any{"@route"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata[decorators] = %#v, want %#v", got, want)
	}
	if got, want := metadata["async"], true; got != want {
		t.Fatalf("metadata[async] = %#v, want %#v", got, want)
	}
	if got, want := metadata["type_annotation_count"], 2; got != want {
		t.Fatalf("metadata[type_annotation_count] = %#v, want %#v", got, want)
	}
	if got, want := metadata["type_annotation_kinds"], []any{"parameter", "return"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata[type_annotation_kinds] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Function handler is async, uses decorators @route, and has parameter and return type annotations."; got != want {
		t.Fatalf("semantic_summary = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("semantic_profile type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["async"], true; got != want {
		t.Fatalf("semantic_profile[async] = %#v, want %#v", got, want)
	}
	if got, want := profile["decorators"], []string{"@route"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("semantic_profile[decorators] = %#v, want %#v", got, want)
	}
	if got, want := profile["type_annotation_count"], 2; got != want {
		t.Fatalf("semantic_profile[type_annotation_count] = %#v, want %#v", got, want)
	}
	if got, want := profile["type_annotation_kinds"], []string{"parameter", "return"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("semantic_profile[type_annotation_kinds] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := result["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("python_semantics type = %T, want map[string]any", result["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("python_semantics[surface_kind] = %#v, want %#v", got, want)
	}
}
