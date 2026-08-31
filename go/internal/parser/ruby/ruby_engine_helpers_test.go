// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeTestFile creates path (and any missing parent directories) with body.
// The parent directory creation is why these tests do not use
// parsertest.WriteFile: several Ruby fixtures live under an app/controllers
// path that must exist before the write.
func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
}

// assertFunctionByName requires payload["functions"] to be a map slice and
// returns the function whose name field equals name.
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

// assertStringFieldValue requires item[field] to equal want, treating a missing
// or non-string field as the empty string so the failure names the wanted value.
func assertStringFieldValue(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, _ := item[field].(string)
	if got != want {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// assertStringSliceNotContains requires item[field] not to contain want. A
// missing or non-slice field passes: these tests use it to assert an absent
// dead-code root, and an unset field is the absent case.
func assertStringSliceNotContains(t *testing.T, item map[string]any, field string, want string) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		return
	}
	for _, value := range got {
		if value == want {
			t.Fatalf("%s = %#v, want not to contain %#v", field, got, want)
		}
	}
}

// repoFixturePath joins parts under the repository's tests/fixtures directory,
// resolved from this file's own location so the tests do not depend on the
// process working directory.
func repoFixturePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) failed")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	elements := append([]string{root, "tests", "fixtures"}, parts...)
	return filepath.Join(elements...)
}
