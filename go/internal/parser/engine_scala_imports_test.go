// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathScalaGroupedImports verifies that Scala grouped
// imports (scala.collection.{mutable, immutable}) produce one import row per
// selector, not a single incomplete row.
func TestDefaultEngineParsePathScalaGroupedImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Grouped.scala")
	writeTestFile(t, sourcePath, `package demo
import scala.collection.{mutable, immutable}
object Demo {
  val buf = mutable.ListBuffer.empty[Int]
}`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "imports", "scala.collection.mutable")
	assertNamedBucketContains(t, got, "imports", "scala.collection.immutable")
}

// TestDefaultEngineParsePathScalaRenamedImports verifies that renamed imports
// (java.util.{List => JList}) use the alias name, not the original.
func TestDefaultEngineParsePathScalaRenamedImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Renamed.scala")
	writeTestFile(t, sourcePath, `package demo
import java.util.{List => JList, Map => JMap}
object Demo {
  val xs: JList[String] = new java.util.ArrayList[String]()
}`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "imports", "java.util.JList")
	assertNamedBucketContains(t, got, "imports", "java.util.JMap")
}

// TestDefaultEngineParsePathScalaWildcardImport verifies that wildcard imports
// (scala.collection._) preserve the wildcard as an import name.
func TestDefaultEngineParsePathScalaWildcardImport(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Wildcard.scala")
	writeTestFile(t, sourcePath, `package demo
import scala.math._
object Demo {
  val x = max(1, 2)
}`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "imports", "scala.math._")
}

// TestDefaultEngineParsePathScalaVariableScopeModule verifies that
// module-scope (default) variable extraction excludes function-local vals.
func TestDefaultEngineParsePathScalaVariableScopeModule(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Scope.scala")
	writeTestFile(t, sourcePath, `object Scope {
  val topLevel = "module"
  def run(): Unit = {
    val local = "function"
  }
}`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "variables", "topLevel")
	assertNamedBucketNotContains(t, got, "variables", "local")
}

// TestDefaultEngineParsePathScalaFunctionDeclaration verifies that abstract
// function declarations (function_declaration, not function_definition) are
// extracted with correct class_context.
func TestDefaultEngineParsePathScalaFunctionDeclaration(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Abstract.scala")
	writeTestFile(t, sourcePath, `trait Service {
  def run(): String
  def status: Int
}
object ServiceImpl extends Service {
  override def run(): String = "ok"
  override def status: Int = 200
}`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	runFn := assertFunctionByNameAndClass(t, got, "run", "Service")
	assertParserStringSliceContains(t, runFn, "dead_code_root_kinds", "scala.trait_method")

	statusFn := assertFunctionByNameAndClass(t, got, "status", "Service")
	assertParserStringSliceContains(t, statusFn, "dead_code_root_kinds", "scala.trait_method")
}

// assertNamedBucketNotContains asserts no item in a named bucket has the given name.
func assertNamedBucketNotContains(t *testing.T, payload map[string]any, bucketKey string, name string) {
	t.Helper()
	bucket, ok := payload[bucketKey].([]map[string]any)
	if !ok {
		return // empty bucket is fine
	}
	for _, item := range bucket {
		if item["name"] == name {
			t.Errorf("unexpected item %q found in bucket %q", name, bucketKey)
		}
	}
}
