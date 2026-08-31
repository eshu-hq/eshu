// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeTestFile writes body to path, creating the parent directories first.
// Several JavaScript/TypeScript fixtures live under nested app/src/server
// layouts that the framework and route detectors key on, so the directory
// must exist before the write. This mirrors the parent parser package's
// writeTestFile, which the relocated external javascript_test package can no
// longer reach.
func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
}

// assertNamedBucketContains requires payload[key] to be a map slice with one
// item whose name field equals wantName.
func assertNamedBucketContains(t *testing.T, payload map[string]any, key string, wantName string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name == wantName {
			return
		}
	}
	t.Fatalf("%s missing name %q in %#v", key, wantName, items)
}

// assertBucketContainsFieldValue requires payload[key] to be a map slice with
// one item whose field equals wantValue.
func assertBucketContainsFieldValue(
	t *testing.T,
	payload map[string]any,
	key string,
	field string,
	wantValue string,
) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		value, _ := item[field].(string)
		if value == wantValue {
			return
		}
	}
	t.Fatalf("%s missing %s=%q in %#v", key, field, wantValue, items)
}

// assertBucketItemByName requires payload[bucket] to be a map slice and
// returns the item whose name field equals name.
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

// assertStringFieldValue requires item[field] to hold the string want.
func assertStringFieldValue(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, _ := item[field].(string)
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// assertStringSliceFieldValue requires item[field] to equal want in order.
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

// writeBenchFile writes contents to path for a benchmark fixture.
func writeBenchFile(b *testing.B, path, contents string) {
	b.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		b.Fatalf("write %s: %v", path, err)
	}
}

// assertFunctionByName requires payload's functions bucket to hold an item
// named name and returns it.
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

// assertNoFrameworkOrNoRoutes requires payload's framework_semantics[section]
// to either be absent or hold no route entries.
func assertNoFrameworkOrNoRoutes(t *testing.T, payload map[string]any, section string) {
	t.Helper()

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics = %T, want map[string]any", payload["framework_semantics"])
	}
	nested, ok := semantics[section].(map[string]any)
	if !ok {
		// Framework not present at all — acceptable
		return
	}
	entries, _ := nested["route_entries"].([]map[string]string)
	if len(entries) > 0 {
		t.Fatalf("framework_semantics.%s.route_entries = %#v, want empty or absent", section, entries)
	}
}

// assertBucketItemByFieldValue requires payload[bucket] to be a map slice and
// returns the item whose field equals want.
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

// assertParserStringSliceContains requires item[field] to be a string slice
// that contains want.
func assertParserStringSliceContains(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	for _, value := range got {
		if value == want {
			return
		}
	}
	t.Fatalf("%s = %#v, want to contain %#v", field, got, want)
}
