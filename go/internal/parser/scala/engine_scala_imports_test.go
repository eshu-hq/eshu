// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scala_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

type parserOptions = parser.Options

func defaultEngine() (*parser.Engine, error) {
	return parser.DefaultEngine()
}

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

	engine, err := defaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, parserOptions{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketNamesEqual(t, got, "imports", []string{
		"scala.collection.immutable",
		"scala.collection.mutable",
	})
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

	engine, err := defaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, parserOptions{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketNamesEqual(t, got, "imports", []string{
		"java.util.JList",
		"java.util.JMap",
	})
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

	engine, err := defaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, parserOptions{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketNamesEqual(t, got, "imports", []string{"scala.math._"})
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

	engine, err := defaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, parserOptions{})
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

	engine, err := defaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, sourcePath, false, parserOptions{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	runFn := assertFunctionByNameAndClass(t, got, "run", "Service")
	assertParserStringSliceContains(t, runFn, "dead_code_root_kinds", "scala.trait_method")

	statusFn := assertFunctionByNameAndClass(t, got, "status", "Service")
	assertParserStringSliceContains(t, statusFn, "dead_code_root_kinds", "scala.trait_method")
}

func assertFunctionByNameAndClass(
	t *testing.T,
	payload map[string]any,
	name string,
	classContext string,
) map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, function := range functions {
		functionName, _ := function["name"].(string)
		functionClassContext, _ := function["class_context"].(string)
		if functionName == name && functionClassContext == classContext {
			return function
		}
	}
	t.Fatalf("functions missing name %q with class_context %q in %#v", name, classContext, functions)
	return nil
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

func assertNamedBucketNamesEqual(t *testing.T, payload map[string]any, key string, want []string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item["name"].(string)
		if !ok {
			t.Fatalf("%s item name = %T, want string", key, item["name"])
		}
		got = append(got, name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s names = %#v, want %#v", key, got, want)
	}
}

func repoFixturePath(pathParts ...string) string {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) failed")
	}

	fixtureParts := []string{filepath.Dir(sourceFile), "..", "..", "..", "..", "tests", "fixtures"}
	return filepath.Join(append(fixtureParts, pathParts...)...)
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	parsertest.WriteFile(t, path, body)
}

func assertNamedBucketContains(t *testing.T, payload map[string]any, key string, wantName string) {
	t.Helper()
	parsertest.AssertNamedBucketContains(t, payload, key, wantName)
}

func assertBucketItemByName(
	t *testing.T,
	payload map[string]any,
	bucket string,
	name string,
) map[string]any {
	t.Helper()
	return parsertest.AssertBucketItemByName(t, payload, bucket, name)
}

func assertParserStringSliceContains(
	t *testing.T,
	item map[string]any,
	field string,
	want string,
) {
	t.Helper()
	parsertest.AssertStringSliceContains(t, item, field, want)
}

func assertFrameworksEqual(t *testing.T, payload map[string]any, want ...string) {
	t.Helper()
	parsertest.AssertFrameworksEqual(t, payload, want...)
}

func assertNestedRouteEntriesEqual(
	t *testing.T,
	payload map[string]any,
	section string,
	want []map[string]string,
) {
	t.Helper()
	parsertest.AssertNestedRouteEntriesEqual(t, payload, section, want)
}
