package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathPythonEmitsAnnotatedAssignmentTypeAnnotations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "annotations.py")
	writeTestFile(
		t,
		filePath,
		`class Settings:
    timeout: int = 30

name: str = "platform"
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{VariableScope: "all"})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	annotations, ok := got["type_annotations"].([]map[string]any)
	if !ok {
		t.Fatalf(`type_annotations = %T, want []map[string]any`, got["type_annotations"])
	}

	want := []map[string]any{
		{
			"name":            "timeout",
			"line_number":     2,
			"type":            "int",
			"annotation_kind": "assignment",
			"context":         "Settings",
			"lang":            "python",
		},
		{
			"name":            "name",
			"line_number":     4,
			"type":            "str",
			"annotation_kind": "assignment",
			"lang":            "python",
		},
	}

	if !reflect.DeepEqual(annotations, want) {
		t.Fatalf("type_annotations = %#v, want %#v", annotations, want)
	}
}
