// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathTypeScriptEmitsImplementedInterfaces(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("sample_projects", "sample_project_typescript")
	filePath := filepath.Join(repoRoot, "src", "classes-inheritance.ts")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	parsed, err := engine.ParsePath(repoRoot, filePath, false, Options{})
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
