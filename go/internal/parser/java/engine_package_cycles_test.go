// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestJavaPackageCyclesParserFlags is the #7349 Java decision gate. A Java
// file's package declaration is its package-cycle identity: the parser
// reports the declared package at the payload top level as package_name so
// the projector can place the file without inventing grouping. Every import
// shape that can close a load-time package cycle is retained and unflagged:
// a plain import, a static member import, and an on-demand import are all
// compile-time dependencies, so none carries type_only, deferred, or
// inferred, and import_type keeps its spelling for consumers that still
// want to tell them apart.
func TestJavaPackageCyclesParserFlags(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "main", "java", "com", "acme", "App.java")
	writeJavaTestFile(t, filePath, `package com.acme;

import com.acme.other.Helper;
import static com.acme.other.Helper.VERSION;
import com.acme.lib.*;

public class App {
    String version() { return VERSION + new Helper().name(); }
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

	if pkg, _ := got["package_name"].(string); pkg != "com.acme" {
		t.Errorf("package_name = %#v, want the file's declared package %q", got["package_name"], "com.acme")
	}

	items, ok := got["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", got["imports"])
	}
	wantType := map[string]string{
		"com.acme.other.Helper":         "import",
		"com.acme.other.Helper.VERSION": "static",
		"com.acme.lib":                  "import",
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

// TestJavaPackageCyclesDefaultPackageCarriesNoPackageName pins the
// absence half of the #7349 package identity: a file outside every package
// declaration carries no package_name key, keeping that payload
// byte-identical, so the projector never invents a package for it.
func TestJavaPackageCyclesDefaultPackageCarriesNoPackageName(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Main.java")
	writeJavaTestFile(t, filePath, `import java.util.List;

public class Main {
    List<String> names();
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
	if value, present := got["package_name"]; present {
		t.Errorf("package_name = %#v without a package declaration, want absent", value)
	}
}
