package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPythonModuleDocstringEmitsModuleMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "module_docstring.py")
	writeTestFile(
		t,
		filePath,
		`"""Utilities for payments."""

def ping():
    return True
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

	moduleItem := assertBucketItemByName(t, got, "modules", "module_docstring")
	assertStringFieldValue(t, moduleItem, "docstring", "Utilities for payments.")
	assertStringFieldValue(t, moduleItem, "lang", "python")
}
