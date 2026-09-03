// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	dynamicVar := findNamedBucketItem(t, got, "variables", "Dynamic")
	assertStringFieldValue(t, dynamicVar, "component_type_assertion", "ComponentType")
}
