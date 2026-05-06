package parser

import "path/filepath"
import "testing"

func TestDefaultEngineParsePathTSXCapturesFragmentAndComponentTypeAssertion(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "Screen.tsx")
	writeTestFile(
		t,
		filePath,
		`import type { ComponentType } from "react";

type ScreenProps = {
  title: string;
};

const Dynamic = component as ComponentType<ScreenProps>;

export function Screen() {
  return (
    <>
      <Header />
      <Dynamic title="ok" />
    </>
  );
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

	dynamicVar := findNamedBucketItem(t, got, "variables", "Dynamic")
	assertStringFieldValue(t, dynamicVar, "component_type_assertion", "ComponentType")

	screenFn := findNamedBucketItem(t, got, "functions", "Screen")
	assertBoolFieldValue(t, screenFn, "jsx_fragment_shorthand", true)

	screenComponent := findNamedBucketItem(t, got, "components", "Screen")
	assertBoolFieldValue(t, screenComponent, "jsx_fragment_shorthand", true)

	assertNamedBucketContains(t, got, "function_calls", "Header")
	assertNamedBucketContains(t, got, "function_calls", "Dynamic")
}

func TestDefaultEngineParsePathTSXCapturesQualifiedComponentTypeAssertion(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "Screen.tsx")
	writeTestFile(
		t,
		filePath,
		`import type * as React from "react";

const Dynamic = component as React.ComponentType<{ title: string }>;
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

	dynamicVar := findNamedBucketItem(t, got, "variables", "Dynamic")
	assertStringFieldValue(t, dynamicVar, "component_type_assertion", "React.ComponentType")
}

func TestDefaultEngineParsePathTSXCapturesParenthesizedQualifiedComponentTypeAssertion(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "Screen.tsx")
	writeTestFile(
		t,
		filePath,
		`import type * as React from "react";

const Dynamic = component as (React.ComponentType<{ title: string }>);
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

	dynamicVar := findNamedBucketItem(t, got, "variables", "Dynamic")
	assertStringFieldValue(t, dynamicVar, "component_type_assertion", "React.ComponentType")
}

func TestDefaultEngineParsePathTSXResolvesComponentTypeAliasImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "Screen.tsx")
	writeTestFile(
		t,
		filePath,
		`import type { ComponentType as CT } from "react";

const Dynamic = component as CT<{ title: string }>;
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

	dynamicVar := findNamedBucketItem(t, got, "variables", "Dynamic")
	assertStringFieldValue(t, dynamicVar, "component_type_assertion", "ComponentType")
}

func assertBoolFieldValue(t *testing.T, item map[string]any, key string, want bool) {
	t.Helper()

	got, ok := item[key].(bool)
	if !ok {
		t.Fatalf("%s = %T, want bool", key, item[key])
	}
	if got != want {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
}
