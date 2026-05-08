package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathTypeScriptCapturesNestedGenericTypeParameters(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "nested_generics.ts")
	writeTestFile(
		t,
		filePath,
		`@sealed
class Demo<T extends Map<string, number> = Map<string, number>> {}

function identity<T extends Record<string, number>>(value: T): T {
  return value;
}

type Box<T extends Result<Map<string, number>, Error>> = { value: T };
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

	demoClass := findNamedBucketItem(t, got, "classes", "Demo")
	classDecorators, ok := demoClass["decorators"].([]string)
	if !ok {
		t.Fatalf("classes.Demo.decorators = %T, want []string", demoClass["decorators"])
	}
	if !reflect.DeepEqual(classDecorators, []string{"@sealed"}) {
		t.Fatalf("classes.Demo.decorators = %#v, want []string{\"@sealed\"}", classDecorators)
	}
	assertStringSliceFieldValue(t, demoClass, "type_parameters", []string{"T"})

	identityFn := findNamedBucketItem(t, got, "functions", "identity")
	assertStringSliceFieldValue(t, identityFn, "type_parameters", []string{"T"})

	boxAlias := findNamedBucketItem(t, got, "type_aliases", "Box")
	assertStringSliceFieldValue(t, boxAlias, "type_parameters", []string{"T"})
}

func TestDefaultEngineParsePathTypeScriptDoesNotTreatReturnTypeArgumentsAsTypeParameters(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "service.ts")
	writeTestFile(
		t,
		filePath,
		`type SearchExecutionResult = { ok: boolean };

export class SearchService {
  async searchText(): Promise<SearchExecutionResult> {
    return { ok: true };
  }
}
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

	searchText := findNamedBucketItem(t, got, "functions", "searchText")
	assertStringSliceFieldValue(t, searchText, "type_parameters", []string{})
}
