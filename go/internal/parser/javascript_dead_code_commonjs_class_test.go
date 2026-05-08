package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaScriptCommonJSDefaultExportClassRootsConstructor(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "errors", "exported-error.js")
	writeTestFile(
		t,
		filePath,
		`class ExportedError extends Error {
  constructor(message) {
    super(message);
  }
}

module.exports = ExportedError;
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

	assertParserStringSliceContains(
		t,
		assertBucketItemByName(t, got, "classes", "ExportedError"),
		"dead_code_root_kinds",
		"javascript.commonjs_default_export",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByNameAndClass(t, got, "constructor", "ExportedError"),
		"dead_code_root_kinds",
		"javascript.commonjs_default_export",
	)
}

func TestDefaultEngineParsePathJavaScriptCommonJSMixinExportRootsMethod(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "salesforce", "order-wrapper-mixin-listing.js")
	writeTestFile(
		t,
		filePath,
		`module.exports.mixin = {};

module.exports.mixin.getFromListing = function(key, defaultValue) {
  return defaultValue;
};
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

	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, got, "getFromListing"),
		"dead_code_root_kinds",
		"javascript.commonjs_mixin_export",
	)
}
