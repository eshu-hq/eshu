// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathKotlinMultilineClassScope(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Repository.kt")
	writeKotlinTreeSitterTestFile(t, sourcePath, `
package demo

interface Service

class Repository(
    private val client: Client,
) : Service {
    fun load(
        id: String,
    ): Result {
        return client.fetch(id)
    }
}

class Client {
    fun fetch(id: String): Result = Result(id)
}

class Result(val id: String)
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertBucketItemByName(t, got, "classes", "Repository")
	load := assertBucketItemByName(t, got, "functions", "load")
	assertStringFieldValue(t, load, "class_context", "Repository")
	assertIntFieldValue(t, load, "line_number", 9)
	assertIntFieldValue(t, load, "end_line", 13)

	call := assertBucketItemByName(t, got, "function_calls", "fetch")
	assertStringFieldValue(t, call, "full_name", "client.fetch")
	assertStringFieldValue(t, call, "inferred_obj_type", "Client")
}

func TestDefaultEngineParsePathKotlinScopesPrimaryConstructorPropertiesToOwningClass(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Repository.kt")
	writeKotlinTreeSitterTestFile(t, sourcePath, `
package demo

class Repository {
    fun load() {
        child.fetch()
    }

    class Nested(
        private val child: ChildClient,
    ) {
        fun loadNested() {
            child.fetch()
        }
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	outerCall := assertKotlinCallByFullNameAndLine(t, got, "child.fetch", 6)
	if _, ok := outerCall["inferred_obj_type"]; ok {
		t.Fatalf("outer call inferred_obj_type = %#v, want absent", outerCall["inferred_obj_type"])
	}

	nestedCall := assertKotlinCallByFullNameAndLine(t, got, "child.fetch", 13)
	assertStringFieldValue(t, nestedCall, "inferred_obj_type", "ChildClient")
}

func assertKotlinCallByFullNameAndLine(t *testing.T, payload map[string]any, fullName string, line int) map[string]any {
	t.Helper()

	calls, ok := payload["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", payload["function_calls"])
	}
	for _, call := range calls {
		gotFullName, _ := call["full_name"].(string)
		gotLine, _ := call["line_number"].(int)
		if gotFullName == fullName && gotLine == line {
			return call
		}
	}
	t.Fatalf("function_calls missing full_name %q on line %d: %#v", fullName, line, calls)
	return nil
}

func writeKotlinTreeSitterTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := ensureParentDirectory(path); err != nil {
		t.Fatalf("ensureParentDirectory(%q) error = %v", path, err)
	}
	if err := osWriteFile(path, []byte(body)); err != nil {
		t.Fatalf("osWriteFile(%q) error = %v", path, err)
	}
}
