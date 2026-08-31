// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parsertest

import (
	"os"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// WriteFile writes body to path with owner-only permissions and fails the test
// if the fixture cannot be created.
func WriteFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
}

// MustParsePath parses filePath through the public default parser engine and
// fails the test if engine construction or parsing returns an error.
func MustParsePath(t *testing.T, repoRoot string, filePath string) map[string]any {
	t.Helper()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}
	return got
}

// AssertNamedBucketContains requires payload[key] to be a map slice with one
// item whose name field equals wantName.
func AssertNamedBucketContains(t *testing.T, payload map[string]any, key string, wantName string) {
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

// AssertBucketItemByName requires payload[key] to be a map slice and returns
// the item whose name field equals wantName.
func AssertBucketItemByName(
	t *testing.T,
	payload map[string]any,
	key string,
	wantName string,
) map[string]any {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name == wantName {
			return item
		}
	}
	t.Fatalf("%s missing name %q in %#v", key, wantName, items)
	return nil
}

// AssertStringSliceContains requires item[field] to be a string slice that
// contains want.
func AssertStringSliceContains(t *testing.T, item map[string]any, field string, want string) {
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

// AssertBucketContainsFieldValue requires payload[key] to be a map slice with
// one item whose field equals wantValue.
func AssertBucketContainsFieldValue(
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

// AssertFrameworksEqual requires framework_semantics.frameworks to equal want
// in order. Calling it without want values requires an empty slice.
func AssertFrameworksEqual(t *testing.T, payload map[string]any, want ...string) {
	t.Helper()

	semantics := frameworkSemanticsMap(t, payload)
	got, ok := semantics["frameworks"].([]string)
	if !ok {
		t.Fatalf("framework_semantics.frameworks = %T, want []string", semantics["frameworks"])
	}
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frameworks = %#v, want %#v", got, want)
	}
}

// AssertNestedStringSliceEqual requires framework_semantics[section][key] to
// equal want in order.
func AssertNestedStringSliceEqual(
	t *testing.T,
	payload map[string]any,
	section string,
	key string,
	want []string,
) {
	t.Helper()

	nested := nestedSemanticsSection(t, payload, section)
	got, ok := nested[key].([]string)
	if !ok {
		t.Fatalf("framework_semantics.%s.%s = %T, want []string", section, key, nested[key])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("framework_semantics.%s.%s = %#v, want %#v", section, key, got, want)
	}
}

// AssertNestedRouteEntriesEqual requires
// framework_semantics[section].route_entries to equal want in order.
func AssertNestedRouteEntriesEqual(
	t *testing.T,
	payload map[string]any,
	section string,
	want []map[string]string,
) {
	t.Helper()

	nested := nestedSemanticsSection(t, payload, section)
	got, ok := nested["route_entries"].([]map[string]string)
	if !ok {
		t.Fatalf("framework_semantics.%s.route_entries = %T, want []map[string]string", section, nested["route_entries"])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("framework_semantics.%s.route_entries = %#v, want %#v", section, got, want)
	}
}

// AssertPrescanContains requires importsMap[name] to contain wantPath. It
// asserts against the map[string][]string shape PreScanPaths returns, not a
// parsed payload.
func AssertPrescanContains(t *testing.T, importsMap map[string][]string, name string, wantPath string) {
	t.Helper()

	paths, ok := importsMap[name]
	if !ok {
		t.Fatalf("imports map missing %q", name)
	}
	for _, path := range paths {
		if path == wantPath {
			return
		}
	}
	t.Fatalf("imports map[%q] = %#v, want path %q", name, paths, wantPath)
}

func frameworkSemanticsMap(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()

	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics = %T, want map[string]any", payload["framework_semantics"])
	}
	return semantics
}

func nestedSemanticsSection(t *testing.T, payload map[string]any, section string) map[string]any {
	t.Helper()

	semantics := frameworkSemanticsMap(t, payload)
	nested, ok := semantics[section].(map[string]any)
	if !ok {
		t.Fatalf("framework_semantics.%s = %T, want map[string]any", section, semantics[section])
	}
	return nested
}
