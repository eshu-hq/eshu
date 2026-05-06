package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaScriptCallMetadataPreservesChainsAndJSXKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "App.tsx")
	writeTestFile(
		t,
		filePath,
		`function Dashboard() {
  service.client.users.list();
  return <Layout.Header />;
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

	assertBucketContainsFieldValue(t, got, "function_calls", "name", "list")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "service.client.users.list")
	assertBucketContainsFieldValue(t, got, "function_calls", "call_kind", "function_call")
	assertBucketContainsFieldValue(t, got, "function_calls", "name", "Header")
	assertBucketContainsFieldValue(t, got, "function_calls", "full_name", "Layout.Header")
	assertBucketContainsFieldValue(t, got, "function_calls", "call_kind", "jsx_component")
}
