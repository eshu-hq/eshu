// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathKotlinMultilineClassScope(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Repository.kt")
	writeKotlinTestFile(t, sourcePath, `
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertBucketItemByName(t, got, "classes", "Repository")
	load := parsertest.AssertBucketItemByName(t, got, "functions", "load")
	parsertest.AssertStringFieldValue(t, load, "class_context", "Repository")
	parsertest.AssertIntFieldValue(t, load, "line_number", 9)
	parsertest.AssertIntFieldValue(t, load, "end_line", 13)

	call := parsertest.AssertBucketItemByName(t, got, "function_calls", "fetch")
	parsertest.AssertStringFieldValue(t, call, "full_name", "client.fetch")
	parsertest.AssertStringFieldValue(t, call, "inferred_obj_type", "Client")
}

func TestDefaultEngineParsePathKotlinScopesPrimaryConstructorPropertiesToOwningClass(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "Repository.kt")
	writeKotlinTestFile(t, sourcePath, `
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	outerCall := assertKotlinCallByFullNameAndLine(t, got, "child.fetch", 6)
	if _, ok := outerCall["inferred_obj_type"]; ok {
		t.Fatalf("outer call inferred_obj_type = %#v, want absent", outerCall["inferred_obj_type"])
	}

	nestedCall := assertKotlinCallByFullNameAndLine(t, got, "child.fetch", 13)
	parsertest.AssertStringFieldValue(t, nestedCall, "inferred_obj_type", "ChildClient")
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
