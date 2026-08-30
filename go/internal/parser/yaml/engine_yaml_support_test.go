// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Support shims for the black-box Engine coverage relocated here from
// internal/parser (issue #6062). The relocated tests drive the public parser
// engine, so they live in the external yaml_test package and reach the engine
// through internal/parser and internal/parser/parsertest.
//
// The wrappers below keep the relocated test bodies at their original call
// spelling so the move stays reviewable as a rename rather than a rewrite.
// Each one delegates to parsertest; only assertEmptyNamedBucket and the
// parent-directory step in writeTestFile have no parsertest equivalent.
package yaml_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// parserOptions aliases the parent engine's option struct so relocated
// ParsePath calls keep their original argument shape.
type parserOptions = parser.Options

// defaultEngine builds the public parser engine the relocated tests drive.
func defaultEngine() (*parser.Engine, error) {
	return parser.DefaultEngine()
}

// writeTestFile creates path's parent directories before writing body. The
// relocated Helm, Kustomize, and environment-overlay tests write into nested
// chart and overlay directories that do not exist inside a fresh t.TempDir,
// which is why this wraps parsertest.WriteFile instead of calling it directly.
func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", dir, err)
	}
	parsertest.WriteFile(t, path, body)
}

// assertNamedBucketContains requires payload[key] to hold an item named
// wantName.
func assertNamedBucketContains(t *testing.T, payload map[string]any, key string, wantName string) {
	t.Helper()

	parsertest.AssertNamedBucketContains(t, payload, key, wantName)
}

// assertBucketContainsFieldValue requires payload[key] to hold an item whose
// field equals wantValue.
func assertBucketContainsFieldValue(
	t *testing.T,
	payload map[string]any,
	key string,
	field string,
	wantValue string,
) {
	t.Helper()

	parsertest.AssertBucketContainsFieldValue(t, payload, key, field, wantValue)
}

// assertEmptyNamedBucket requires payload[key] to be a present but empty
// bucket. parsertest has no equivalent, and the Flux misroute regressions rely
// on the distinction between an absent bucket and an empty one.
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

// findNamedItem returns the item named name from payload[bucket], failing the
// test when the bucket is the wrong type or holds no such item.
func findNamedItem(t *testing.T, payload map[string]any, bucket string, name string) map[string]any {
	t.Helper()

	return parsertest.AssertBucketItemByName(t, payload, bucket, name)
}
