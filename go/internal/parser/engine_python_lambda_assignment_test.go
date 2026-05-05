package parser

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultEngineParsePathPythonLambdaAttributeAssignmentEmitsNamedFunction(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "lambda_attribute.py")
	writeTestFile(
		t,
		filePath,
		`service.handler = lambda request: request
service.another = lambda value, flag: value if flag else value
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

	handlerFn := assertFunctionByName(t, got, "service.handler")
	assertStringFieldValue(t, handlerFn, "semantic_kind", "lambda")
	args, ok := handlerFn["args"].([]string)
	if !ok {
		t.Fatalf(`functions["service.handler"]["args"] = %T, want []string`, handlerFn["args"])
	}
	if !reflect.DeepEqual(args, []string{"request"}) {
		t.Fatalf(`functions["service.handler"]["args"] = %#v, want []string{"request"}`, args)
	}

	anotherFn := assertFunctionByName(t, got, "service.another")
	assertStringFieldValue(t, anotherFn, "semantic_kind", "lambda")
	anotherArgs, ok := anotherFn["args"].([]string)
	if !ok {
		t.Fatalf(`functions["service.another"]["args"] = %T, want []string`, anotherFn["args"])
	}
	if !reflect.DeepEqual(anotherArgs, []string{"value", "flag"}) {
		t.Fatalf(`functions["service.another"]["args"] = %#v, want []string{"value", "flag"}`, anotherArgs)
	}
}

func TestDefaultEngineParsePathPythonAnonymousLambdaPromotesSyntheticFunction(
	t *testing.T,
) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "anonymous_lambda.py")
	writeTestFile(
		t,
		filePath,
		`def build_predicate():
    return lambda value: value + 1
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

	found := false
	for _, item := range got["functions"].([]map[string]any) {
		name, _ := item["name"].(string)
		if strings.HasPrefix(name, "lambda@") {
			found = true
			assertStringFieldValue(t, item, "semantic_kind", "lambda")
			args, ok := item["args"].([]string)
			if !ok {
				t.Fatalf(`functions[%q]["args"] = %T, want []string`, name, item["args"])
			}
			if !reflect.DeepEqual(args, []string{"value"}) {
				t.Fatalf(`functions[%q]["args"] = %#v, want []string{"value"}`, name, args)
			}
			break
		}
	}
	if !found {
		t.Fatal("anonymous lambda function not found")
	}
}
