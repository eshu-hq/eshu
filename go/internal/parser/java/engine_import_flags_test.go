// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestDefaultEngineParsePathJavaImportsCarryNoFlags is the #7344 Java decision
// gate. Java has no type-only or deferred import, and a static import is a
// compile-time dependency like any other, so a plain, a static, and an on-demand
// import are all retained and none is flagged. import_type keeps the static
// spelling so a consumer can still tell them apart.
func TestDefaultEngineParsePathJavaImportsCarryNoFlags(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "main", "java", "com", "acme", "App.java")
	writeJavaTestFile(t, filePath, `package com.acme;

import java.util.List;
import static java.lang.Math.max;
import com.acme.other.*;

public class App {
    int biggest(List<Integer> xs) { return max(xs.get(0), xs.get(1)); }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	}
	wantType := map[string]string{
		"java.util.List":     "import",
		"java.lang.Math.max": "static",
		"com.acme.other":     "import",
	}
	found := 0
	for _, item := range items {
		source, _ := item["source"].(string)
		for _, flag := range []string{"type_only", "deferred", "inferred"} {
			if value, present := item[flag]; present {
				t.Errorf("java import %q flag %q = %#v, want absent", source, flag, value)
			}
		}
		for prefix, importType := range wantType {
			if source == prefix || source == prefix+".*" {
				found++
				if got, _ := item["import_type"].(string); got != importType {
					t.Errorf("java import %q import_type = %q, want %q", source, got, importType)
				}
			}
		}
	}
	if found != len(wantType) {
		t.Errorf("matched %d of %d expected java imports in %#v", found, len(wantType), items)
	}
}
