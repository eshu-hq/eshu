// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
// noFrameworkOrNoRoutes reports why payload fails the "no framework, or no
// route entries" expectation, or nil when it holds. The predicate is split out
// of the assertion so its fail-closed branches can be exercised directly: the
// assertion takes a *testing.T and calls t.Fatalf, so a test cannot drive its
// failure paths without a fake T, and an unexercised fail-closed branch is
// free to rot back into the false green it was written to prevent.
func noFrameworkOrNoRoutes(payload map[string]any, section string) error {
	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		return fmt.Errorf("framework_semantics = %#v (%T), want map[string]any", payload["framework_semantics"], payload["framework_semantics"])
	}
	rawSection, present := semantics[section]
	if !present {
		// Framework not present at all — acceptable.
		return nil
	}
	nested, ok := rawSection.(map[string]any)
	if !ok {
		return fmt.Errorf("framework_semantics.%s = %#v (%T), want map[string]any; a present-but-malformed section must not pass a negative assertion", section, rawSection, rawSection)
	}
	rawEntries, present := nested["route_entries"]
	if !present {
		return nil
	}
	entries, ok := rawEntries.([]map[string]string)
	if !ok {
		return fmt.Errorf("framework_semantics.%s.route_entries = %#v (%T), want []map[string]string; a present-but-malformed field must not pass a negative assertion", section, rawEntries, rawEntries)
	}
	if len(entries) > 0 {
		return fmt.Errorf("framework_semantics.%s.route_entries = %#v, want empty or absent", section, entries)
	}
	return nil
}

func assertNoFrameworkOrNoRoutes(t *testing.T, payload map[string]any, section string) {
	t.Helper()

	if err := noFrameworkOrNoRoutes(payload, section); err != nil {
		t.Fatalf("%v", err)
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

// TestNoFrameworkOrNoRoutesFailsClosedOnMalformedInput is the positive control
// for the two fail-closed branches. Before those branches existed, each of
// these payloads produced a nil zero value from a discarded type assertion and
// passed the negative assertion, which is the false green being guarded
// against. Without this test, reverting either check to `x, _ := ...` would
// reopen that hole silently.
func TestNoFrameworkOrNoRoutesFailsClosedOnMalformedInput(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "section present but not a map",
			payload: map[string]any{
				"framework_semantics": map[string]any{"express": "not-a-map"},
			},
		},
		{
			name: "route_entries present but wrongly typed",
			payload: map[string]any{
				"framework_semantics": map[string]any{
					"express": map[string]any{
						"route_entries": []map[string]any{{"path": "/x"}},
					},
				},
			},
		},
		{
			name:    "framework_semantics itself not a map",
			payload: map[string]any{"framework_semantics": []string{"express"}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := noFrameworkOrNoRoutes(tt.payload, "express"); err == nil {
				t.Fatalf("noFrameworkOrNoRoutes(%#v) = nil, want an error: a present-but-malformed value must not pass a negative assertion", tt.payload)
			}
		})
	}
}

// TestNoFrameworkOrNoRoutesAcceptsAbsentAndEmpty pins the accepting side, so
// the fail-closed branches above cannot be satisfied by a predicate that
// simply rejects everything.
func TestNoFrameworkOrNoRoutesAcceptsAbsentAndEmpty(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		payload map[string]any
	}{
		{
			name:    "framework absent",
			payload: map[string]any{"framework_semantics": map[string]any{}},
		},
		{
			name: "framework present, route_entries absent",
			payload: map[string]any{
				"framework_semantics": map[string]any{"express": map[string]any{}},
			},
		},
		{
			name: "route_entries present and empty",
			payload: map[string]any{
				"framework_semantics": map[string]any{
					"express": map[string]any{"route_entries": []map[string]string{}},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := noFrameworkOrNoRoutes(tt.payload, "express"); err != nil {
				t.Fatalf("noFrameworkOrNoRoutes(%#v) = %v, want nil", tt.payload, err)
			}
		})
	}
}

// repoFixturePath resolves a path under tests/fixtures relative to the repo
// root. It mirrors the parent parser package's repoFixturePath, which the
// relocated external javascript_test package can no longer reach. The
// javascript package sits one directory deeper than the parent, so the walk
// up to the repo root takes four ".." elements instead of the parent's three.
func repoFixturePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) failed")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	elements := append([]string{root, "tests", "fixtures"}, parts...)
	return filepath.Join(elements...)
}

// assertFunctionByNameAndClass returns the functions-bucket item matching both
// name and class_context. It mirrors the parent parser package's helper of the
// same name, which the relocated external javascript_test package can no
// longer reach: parsertest imports internal/parser, so an internal `package
// parser` test importing parsertest is a cycle, and that constraint carries
// over to why this copy exists here rather than being shared.
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

// assertParserStringSliceFieldValue is the name the javascript_dead_code_*
// engine tests use for assertStringSliceFieldValue. It mirrors the parent
// parser package's helper of the same name, kept as a separate name so the
// relocated callers stay a pure move.
func assertParserStringSliceFieldValue(t *testing.T, item map[string]any, field string, want []string) {
	t.Helper()

	assertStringSliceFieldValue(t, item, field, want)
}

// assertBoolFieldValue requires item[key] to hold the bool want. The TSX
// suites relocated here assert boolean shape flags such as
// jsx_fragment_shorthand, and parsertest has no bool variant, so this package
// carries its own. The parent parser package keeps a copy for the NuGet
// tests that stay at root.
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
