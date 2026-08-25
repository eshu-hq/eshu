// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func javaFixturePath(t *testing.T, pathParts ...string) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || sourceFile == "" {
		t.Fatal("runtime.Caller(0) could not locate Java test source file")
		return ""
	}

	fixtureParts := []string{filepath.Dir(sourceFile), "..", "..", "..", "..", "tests", "fixtures"}
	return filepath.Join(append(fixtureParts, pathParts...)...)
}

func TestDefaultEngineParsePathJavaEmitsImplementedInterfaces(t *testing.T) {
	t.Parallel()

	repoRoot := javaFixturePath(t, "sample_projects", "sample_project_java")
	filePath := filepath.Join(repoRoot, "src", "com", "example", "app", "service", "impl", "GreetingServiceImpl.java")

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
	var impl map[string]any
	for _, class := range classes {
		if class["name"] == "GreetingServiceImpl" {
			impl = class
		}
	}
	if impl == nil {
		t.Fatalf("class GreetingServiceImpl not found in %#v", classes)
	}

	interfaces, ok := impl["implemented_interfaces"].([]string)
	if !ok {
		t.Fatalf("implemented_interfaces type = %T, want []string", impl["implemented_interfaces"])
	}
	if len(interfaces) != 1 || interfaces[0] != "GreetingService" {
		t.Fatalf("implemented_interfaces = %#v, want [GreetingService]", interfaces)
	}
}
