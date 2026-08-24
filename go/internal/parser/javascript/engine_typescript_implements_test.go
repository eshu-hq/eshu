// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDefaultEngineParsePathTypeScriptEmitsImplementedInterfaces(t *testing.T) {
	t.Parallel()

	repoRoot := sampleTypeScriptFixturePath(t)
	filePath := filepath.Join(repoRoot, "src", "classes-inheritance.ts")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	parsed, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	classes, ok := parsed["classes"].([]map[string]any)
	if !ok {
		t.Fatalf("classes type = %T, want []map[string]any", parsed["classes"])
	}
	var duck map[string]any
	for _, class := range classes {
		if class["name"] == "Duck" {
			duck = class
		}
	}
	if duck == nil {
		t.Fatalf("class Duck not found in %#v", classes)
	}

	interfaces, ok := duck["implemented_interfaces"].([]string)
	if !ok {
		t.Fatalf("implemented_interfaces type = %T, want []string", duck["implemented_interfaces"])
	}
	if len(interfaces) != 2 || interfaces[0] != "Flyable" || interfaces[1] != "Swimmable" {
		t.Fatalf("implemented_interfaces = %#v, want [Flyable Swimmable]", interfaces)
	}
}

func sampleTypeScriptFixturePath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
		return ""
	}

	return filepath.Join(
		filepath.Dir(file), "..", "..", "..", "..",
		"tests", "fixtures", "sample_projects", "sample_project_typescript",
	)
}
