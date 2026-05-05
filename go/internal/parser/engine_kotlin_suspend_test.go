package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathKotlinMarksSuspendFunctions(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Coroutine.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Worker {
    suspend fun load(): String = "ok"
}

suspend fun fetchRemote(): String = "remote"

fun regular(): String = "done"
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

	load := assertBucketItemByName(t, got, "functions", "load")
	assertBoolFieldValue(t, load, "suspend", true)

	fetchRemote := assertBucketItemByName(t, got, "functions", "fetchRemote")
	assertBoolFieldValue(t, fetchRemote, "suspend", true)
}
