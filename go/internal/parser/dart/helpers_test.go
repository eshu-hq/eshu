// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSource(t *testing.T, name string, source string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return path
}

func assertBucketName(t *testing.T, payload map[string]any, bucket string, name string) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if item["name"] == name {
			return item
		}
	}
	t.Fatalf("payload[%q] missing name %q in %#v", bucket, name, items)
	return nil
}

func assertBucketNameMissing(t *testing.T, payload map[string]any, bucket string, name string) {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if item["name"] == name {
			t.Fatalf("payload[%q] contains name %q in %#v", bucket, name, items)
		}
	}
}

func assertFunctionByNameAndClass(t *testing.T, payload map[string]any, name string, classContext string) map[string]any {
	t.Helper()

	items, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[functions] = %T, want []map[string]any", payload["functions"])
	}
	for _, item := range items {
		if item["name"] == name && item["class_context"] == classContext {
			return item
		}
	}
	t.Fatalf("functions missing name %q class_context %q in %#v", name, classContext, items)
	return nil
}

func assertBucketNameCount(t *testing.T, payload map[string]any, bucket string, name string, want int) {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	count := 0
	for _, item := range items {
		if item["name"] == name {
			count++
		}
	}
	if count != want {
		t.Fatalf("payload[%q] name %q count = %d, want %d in %#v", bucket, name, count, want, items)
	}
}

func assertStringSliceContains(t *testing.T, item map[string]any, key string, want string) {
	t.Helper()

	values, ok := item[key].([]string)
	if !ok {
		t.Fatalf("item[%q] = %T, want []string in %#v", key, item[key], item)
	}
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("item[%q] missing %q in %#v", key, want, values)
}

func assertStringSliceNotContains(t *testing.T, item map[string]any, key string, unwanted string) {
	t.Helper()

	values, _ := item[key].([]string)
	for _, value := range values {
		if value == unwanted {
			t.Fatalf("item[%q] contains %q in %#v", key, unwanted, values)
		}
	}
}
