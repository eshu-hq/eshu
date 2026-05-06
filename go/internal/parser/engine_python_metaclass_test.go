package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathPythonEmitsMetaclassMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "metaclass.py")
	writeTestFile(
		t,
		filePath,
		`class MetaLogger(type):
    pass

class Logged(metaclass=MetaLogger):
    pass
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	logged := assertBucketItemByName(t, got, "classes", "Logged")
	assertStringFieldValue(t, logged, "metaclass", "MetaLogger")
}

func TestDefaultEngineParsePathPythonLambdaAssignmentEmitsNamedFunction(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "lambda_assignment.py")
	writeTestFile(
		t,
		filePath,
		`double = lambda x: x * 2
add = lambda x, y: x + y
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	doubleFn := assertFunctionByName(t, got, "double")
	assertStringFieldValue(t, doubleFn, "semantic_kind", "lambda")
	args, ok := doubleFn["args"].([]string)
	if !ok {
		t.Fatalf(`functions["double"]["args"] = %T, want []string`, doubleFn["args"])
	}
	if !reflect.DeepEqual(args, []string{"x"}) {
		t.Fatalf(`functions["double"]["args"] = %#v, want []string{"x"}`, args)
	}

	addFn := assertFunctionByName(t, got, "add")
	assertStringFieldValue(t, addFn, "semantic_kind", "lambda")
	addArgs, ok := addFn["args"].([]string)
	if !ok {
		t.Fatalf(`functions["add"]["args"] = %T, want []string`, addFn["args"])
	}
	if !reflect.DeepEqual(addArgs, []string{"x", "y"}) {
		t.Fatalf(`functions["add"]["args"] = %#v, want []string{"x", "y"}`, addArgs)
	}
}
