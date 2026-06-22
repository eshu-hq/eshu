package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathKotlinExtractsImportedBareCalls proves that a
// receiver-less call to an explicitly imported top-level function still emits a
// function_calls row. Kotlin imports do not declare function-vs-class, so the
// import alias must not be treated as a constructor target that suppresses the
// call edge. Only locally-declared types are constructor candidates. Reported as
// a P2 on PR #3528.
func TestDefaultEngineParsePathKotlinExtractsImportedBareCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "consumer.kt")
	writeTestFile(
		t,
		filePath,
		`package demo

import demo.util.helper
import demo.util.Widget

fun run(): String {
    val w = Widget()
    return helper(w)
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	// The imported top-level function call must emit a call edge.
	assertNamedBucketContains(t, payload, "function_calls", "helper")
	// The imported type constructed with `Widget()` is still emitted by the
	// constructor-call path, not the bare-call path.
	assertNamedBucketContains(t, payload, "function_calls", "Widget")
}
