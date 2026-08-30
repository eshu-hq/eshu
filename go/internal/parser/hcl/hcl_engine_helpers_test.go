// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// writeTestFile creates any missing parent directories before writing body to
// path. The relocated backend tests place fixtures at nested paths such as
// env/prod/main.tf, and parsertest.WriteFile does not create directories.
func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	parsertest.WriteFile(t, path, body)
}

// assertEmptyNamedBucket requires payload[key] to be a map slice holding no
// items. parsertest carries no empty-bucket assertion, and only the Terraform
// modern-block coverage needs one, so it stays local to this package.
func assertEmptyNamedBucket(t *testing.T, payload map[string]any, key string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	if len(items) != 0 {
		t.Fatalf("%s = %#v, want empty bucket", key, items)
	}
}
