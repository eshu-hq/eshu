// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func ensureParentDirectory(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

func osWriteFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o644)
}

func repoFixturePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) failed")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	elements := append([]string{root, "tests", "fixtures"}, parts...)
	return filepath.Join(elements...)
}

// assertStringSliceFieldValue requires item[field] to equal want in order. It
// lives here rather than in a language-specific test file because several
// engine test files assert ordered string-slice payload fields.
func assertStringSliceFieldValue(
	t *testing.T,
	item map[string]any,
	field string,
	want []string,
) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// assertFunctionByName returns the functions-bucket item matching name. It
// lives here rather than in a language-specific test file because C#, Go,
// Groovy, Java, JavaScript, Kotlin, PHP, and SQL engine test files all
// look up functions by name alone.
func assertFunctionByName(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, function := range functions {
		functionName, _ := function["name"].(string)
		if functionName == name {
			return function
		}
	}
	t.Fatalf("functions missing name %q in %#v", name, functions)
	return nil
}

// assertBucketItemByName returns the payload[bucket] item matching name. It
// lives here rather than in a language-specific test file for the same
// cross-language reason as assertFunctionByName.
func assertBucketItemByName(t *testing.T, payload map[string]any, bucket string, name string) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		itemName, _ := item["name"].(string)
		if itemName == name {
			return item
		}
	}
	t.Fatalf("%s missing name %q in %#v", bucket, name, items)
	return nil
}

// assertStringFieldValue requires item[field] to hold the string want. It
// lives here rather than in a language-specific test file for the same
// cross-language reason as assertFunctionByName.
func assertStringFieldValue(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, _ := item[field].(string)
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// assertIntFieldValue requires item[field] to hold the int want. It lives
// here rather than in a language-specific test file for the same
// cross-language reason as assertFunctionByName.
func assertIntFieldValue(t *testing.T, item map[string]any, field string, want int) {
	t.Helper()

	got, ok := item[field].(int)
	if !ok {
		t.Fatalf("%s = %T, want int", field, item[field])
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", field, got, want)
	}
}

// assertBucketItemByFieldValue returns the payload[bucket] item whose string
// field equals want. It lives here rather than in a language-specific test
// file because the Go, Java, JavaScript, and TypeScript engine tests key
// function_calls rows by full_name or call_kind rather than by name. The
// external internal/parser/php tests use parsertest.AssertBucketItemByFieldValue
// instead. This copy cannot be replaced by that shared one: parsertest imports
// this package, so an internal `package parser` test importing parsertest is an
// import cycle. Only the external <lang>_test child packages can take the
// shared helper.
func assertBucketItemByFieldValue(
	t *testing.T,
	payload map[string]any,
	bucket string,
	field string,
	want string,
) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		value, _ := item[field].(string)
		if value == want {
			return item
		}
	}
	t.Fatalf("%s missing %s=%q in %#v", bucket, field, want, items)
	return nil
}

// assertFunctionByNameAndClass returns the functions-bucket item matching both
// name and class_context. It lives here rather than in a language-specific
// test file because the C#, Java, JavaScript, Kotlin, and Swift engine tests
// all reuse method names across classes. The external internal/parser/golang
// tests use parsertest.AssertFunctionByNameAndClass instead; this copy cannot
// be replaced by that shared one for the import-cycle reason given on
// assertBucketItemByFieldValue.
func assertFunctionByNameAndClass(t *testing.T, payload map[string]any, name string, classContext string) map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, function := range functions {
		functionName, isString := function["name"].(string)
		if raw, present := function["name"]; present && !isString {
			t.Fatalf("functions item has name = %#v (%T), want string; a present-but-malformed field must not be silently treated as the empty string", raw, raw)
		}
		functionClassContext, isString := function["class_context"].(string)
		if raw, present := function["class_context"]; present && !isString {
			t.Fatalf("functions item has class_context = %#v (%T), want string; a present-but-malformed field must not be silently treated as the empty string", raw, raw)
		}
		if functionName == name && functionClassContext == classContext {
			return function
		}
	}
	t.Fatalf("functions missing name %q with class_context %q in %#v", name, classContext, functions)
	return nil
}

// writeBenchFile writes contents to path and fails the benchmark if the write
// fails. It lives here rather than in a language-specific benchmark file
// because the content-metadata and JavaScript parent-lookup benchmarks share it.
func writeBenchFile(b *testing.B, path, contents string) {
	b.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		b.Fatalf("write %s: %v", path, err)
	}
}
