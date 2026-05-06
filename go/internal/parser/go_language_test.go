package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoPreservesSelectorCallContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.go")
	writeTestFile(
		t,
		filePath,
		`package main

import "fmt"

func main() {
	fmt.Println("hello")
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

	call := assertBucketItemByName(t, got, "function_calls", "Println")
	assertStringFieldValue(t, call, "full_name", "fmt.Println")
}
