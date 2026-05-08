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

func TestDefaultEngineParsePathTypeScriptNewExpressionReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "app.ts")
	writeTestFile(
		t,
		filePath,
		`import { Worker } from "./worker";

export const main = async () => {
  const worker = new Worker();
  await worker.invoke();
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

	constructorCall := assertBucketItemByFieldValue(t, got, "function_calls", "call_kind", "constructor_call")
	assertStringFieldValue(t, constructorCall, "name", "Worker")
	assertStringFieldValue(t, constructorCall, "full_name", "Worker")

	invokeCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "worker.invoke")
	assertStringFieldValue(t, invokeCall, "name", "invoke")
	assertStringFieldValue(t, invokeCall, "inferred_obj_type", "Worker")
}
