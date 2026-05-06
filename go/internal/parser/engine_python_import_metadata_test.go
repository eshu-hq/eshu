package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPythonEmitsImportSourceAndAliasMetadata(
	t *testing.T,
) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "imports.py")
	writeTestFile(
		t,
		filePath,
		`from lib.factory import create_app as make_app, helper
import pkg.mod as mod
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

	createApp := assertBucketItemByName(t, got, "imports", "create_app")
	assertStringFieldValue(t, createApp, "alias", "make_app")
	assertStringFieldValue(t, createApp, "source", "lib.factory")

	helper := assertBucketItemByName(t, got, "imports", "helper")
	assertStringFieldValue(t, helper, "source", "lib.factory")

	moduleAlias := assertBucketItemByName(t, got, "imports", "pkg.mod")
	assertStringFieldValue(t, moduleAlias, "alias", "mod")
	assertStringFieldValue(t, moduleAlias, "source", "pkg.mod")
}
