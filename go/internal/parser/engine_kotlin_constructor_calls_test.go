package parser

import (
	"path/filepath"
	"testing"
)

func TestParseKotlinCapturesPrimaryConstructorCallsInFunctionBodies(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	source := `package comprehensive

class Person(val name: String, val age: Int) {
    companion object {
        fun create(name: String): Person = Person(name, 0)
    }

    fun greet(): String = "Hi, I'm $name"
}
`
	path := filepath.Join(repoRoot, "Classes.kt")
	writeTestFile(t, path, source)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, path, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", path, err)
	}

	calls, ok := payload["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", payload["function_calls"])
	}
	if len(calls) != 1 {
		t.Fatalf("len(function_calls) = %d, want 1; function_calls=%#v", len(calls), payload["function_calls"])
	}
	assertStringFieldValue(t, calls[0], "name", "Person")
	assertIntFieldValue(t, calls[0], "line_number", 5)
}
