// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestParseKotlinCapturesPrimaryConstructorCallsInFunctionBodies(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	source := `package comprehensive

class Person(val name: String, val age: Int) {
    companion object {
        fun create(name: String): Person = Person(name, 0)
    }

    fun greet(): String = "Hi, I'm $name"
}
`
	path := filepath.Join(repoRoot, "Classes.kt")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, path, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", path, err)
	}

	calls, ok := payload["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", payload["function_calls"])
	}
	if len(calls) != 1 {
		t.Fatalf("len(function_calls) = %d, want 1; function_calls=%#v", len(calls), payload["function_calls"])
	}
	name, ok := calls[0]["name"].(string)
	if !ok || name != "Person" {
		t.Fatalf("function_calls[0].name = %#v, want %q", calls[0]["name"], "Person")
	}
	lineNumber, ok := calls[0]["line_number"].(int)
	if !ok || lineNumber != 5 {
		t.Fatalf("function_calls[0].line_number = %#v, want 5", calls[0]["line_number"])
	}
}
