package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathKotlinExtractsBareCalls proves that unqualified
// (receiver-less) Kotlin function calls such as same-scope method calls,
// top-level function calls, and imported function calls are extracted. Before
// this, the Kotlin call pattern required a `receiver.method` shape, so bare
// calls like `info(...)` or `println(...)` were silently dropped. Issue #3486
// requires Kotlin to emit calls.
func TestDefaultEngineParsePathKotlinExtractsBareCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "service.kt")
	writeTestFile(
		t,
		filePath,
		`package demo

fun helper(value: String): String = value

class Service {
    fun handle(input: String): String {
        log("handling")
        val result = helper(input)
        return result
    }

    fun log(message: String) {
        println(message)
    }
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

	for _, want := range []string{"log", "helper", "println"} {
		assertNamedBucketContains(t, payload, "function_calls", want)
	}
}
