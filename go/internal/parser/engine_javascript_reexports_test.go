package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaScriptStaticRelativeReExports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "resources", "index.ts")
	writeTestFile(
		t,
		filePath,
		`export { encode } from "./jwt";
export { decode as verify } from "./jwt";
export * from "./ssm";
export { remote } from "@vendor/remote";
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

	encode := findNamedBucketItem(t, got, "imports", "encode")
	assertStringFieldValue(t, encode, "source", "./jwt")
	assertStringFieldValue(t, encode, "import_type", "reexport")
	assertStringFieldValue(t, encode, "original_name", "encode")

	verify := findNamedBucketItem(t, got, "imports", "verify")
	assertStringFieldValue(t, verify, "source", "./jwt")
	assertStringFieldValue(t, verify, "import_type", "reexport")
	assertStringFieldValue(t, verify, "original_name", "decode")

	star := findNamedBucketItem(t, got, "imports", "*")
	assertStringFieldValue(t, star, "source", "./ssm")
	assertStringFieldValue(t, star, "import_type", "reexport")

	assertBucketMissingFieldValue(t, got, "imports", "source", "@vendor/remote")
}

func assertBucketMissingFieldValue(t *testing.T, payload map[string]any, key string, field string, value string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		if got, _ := item[field].(string); got == value {
			t.Fatalf("%s contains %s=%q in %#v, want missing", key, field, value, items)
		}
	}
}
