package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPythonGeneratorFunctionsEmitSemanticKind(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "generators.py")
	writeTestFile(
		t,
		filePath,
		`def create_ids():
    yield 1
    yield 2
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

	fn := assertFunctionByName(t, got, "create_ids")
	assertStringFieldValue(t, fn, "semantic_kind", "generator")
}

func TestDefaultEngineParsePathPythonGeneratorYieldInNestedFunctionStaysInnerOnly(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "nested_generators.py")
	writeTestFile(
		t,
		filePath,
		`def outer():
    def inner():
        yield 1
    return 1

def create_ids():
    yield 1
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

	outer := assertFunctionByName(t, got, "outer")
	if _, ok := outer["semantic_kind"]; ok {
		t.Fatalf("outer semantic_kind = %#v, want absent", outer["semantic_kind"])
	}

	inner := assertFunctionByName(t, got, "inner")
	assertStringFieldValue(t, inner, "semantic_kind", "generator")

	createIDs := assertFunctionByName(t, got, "create_ids")
	assertStringFieldValue(t, createIDs, "semantic_kind", "generator")
}
