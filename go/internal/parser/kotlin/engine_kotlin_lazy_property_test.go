// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathKotlinInfersLazyDelegatedPropertyReceiverTypesForDotCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun createService(): Service = Service()

fun usage(): String {
    val service by lazy { createService() }
    return service.info()
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

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "service.info" {
			continue
		}
		parsertest.AssertStringFieldValue(t, item, "inferred_obj_type", "Service")
		parsertest.AssertStringFieldValue(t, item, "call_kind", "kotlin_lazy_delegated_property_receiver")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "service.info", items)
}

// TestDefaultEngineParsePathKotlinEndLineSpansMultilineProperty pins that a
// Kotlin property spanning several lines (here `by lazy { ... }`) records its
// full span: end_line is the declaration's last line, not a copy of
// line_number (#7686). A single-line property keeps a single-line span.
func TestDefaultEngineParsePathKotlinEndLineSpansMultilineProperty(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Props.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package spans

val eager: String = "x"
val deferred by lazy {
    compute()
}

fun compute(): String = "c"
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

	eager := parsertest.AssertBucketItemByName(t, got, "variables", "eager")
	parsertest.AssertIntFieldValue(t, eager, "line_number", 3)
	parsertest.AssertIntFieldValue(t, eager, "end_line", 3)

	deferred := parsertest.AssertBucketItemByName(t, got, "variables", "deferred")
	parsertest.AssertIntFieldValue(t, deferred, "line_number", 4)
	parsertest.AssertIntFieldValue(t, deferred, "end_line", 6)
}
