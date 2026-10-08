// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathKotlinInterfaceMembersCarryTypeContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Service.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

interface IService {
    fun execute(): String = "ok"
}

class Service : IService {
    override fun execute(): String = "ok"
}

fun createService(): IService = Service()

fun usage(): String {
    val service = createService()
    return service.execute()
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertNamedBucketContains(t, got, "interfaces", "IService")
	parsertest.AssertFunctionByNameAndClass(t, got, "execute", "IService")
	parsertest.AssertBucketContainsFieldValue(t, got, "function_calls", "full_name", "service.execute")
	parsertest.AssertBucketContainsFieldValue(t, got, "function_calls", "inferred_obj_type", "IService")
}

// TestDefaultEngineParsePathKotlinEndLineSpansTypeDeclarations pins that
// Kotlin class and interface declarations record their full span: end_line is
// the declaration's last line, not a copy of line_number (#7686). The reducer
// uses the span to find the type containing a call, so a single-line span
// drops every call in the class body that sits outside a function (for
// example a property initializer).
func TestDefaultEngineParsePathKotlinEndLineSpansTypeDeclarations(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Spans.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package spans

class Greeter {
    fun greet(): String {
        return "hi"
    }
}

interface Speaker {
    fun speak(): String
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	greeter := parsertest.AssertBucketItemByName(t, got, "classes", "Greeter")
	parsertest.AssertIntFieldValue(t, greeter, "line_number", 3)
	parsertest.AssertIntFieldValue(t, greeter, "end_line", 7)

	speaker := parsertest.AssertBucketItemByName(t, got, "interfaces", "Speaker")
	parsertest.AssertIntFieldValue(t, speaker, "line_number", 9)
	parsertest.AssertIntFieldValue(t, speaker, "end_line", 11)
}
