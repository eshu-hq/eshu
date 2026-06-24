// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultEngineParsePathKotlinComprehensiveFixturesParseCleanly walks every
// Kotlin source file under the committed fixture corpora and asserts the
// tree-sitter AST parser returns a payload without error. It guards the AST
// rewrite (issue #3533) against grammar constructs that would previously fall
// back to regex/line-scan handling.
func TestDefaultEngineParsePathKotlinComprehensiveFixturesParseCleanly(t *testing.T) {
	t.Parallel()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	roots := []string{
		repoFixturePath("ecosystems", "kotlin_comprehensive"),
		repoFixturePath("sample_projects", "sample_project_kotlin"),
		repoFixturePath("deadcode", "kotlin"),
	}
	for _, root := range roots {
		root := root
		err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() || !strings.HasSuffix(path, ".kt") {
				return walkErr
			}
			payload, parseErr := engine.ParsePath(root, path, false, Options{})
			if parseErr != nil {
				t.Errorf("ParsePath(%q) error = %v, want nil", path, parseErr)
				return nil
			}
			if _, ok := payload["functions"].([]map[string]any); !ok {
				t.Errorf("ParsePath(%q) missing functions bucket", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Walk(%q) error = %v, want nil", root, err)
		}
	}
}

// TestDefaultEngineParsePathKotlinSmartCastDoesNotLeakAcrossBranches verifies
// that an `is` narrowing applied inside one guarded block does not bleed into a
// sibling statement after the block closes. The AST walker scopes the cast to
// the guarded subtree, so the post-block call carries no inferred type.
func TestDefaultEngineParsePathKotlinSmartCastDoesNotLeakAcrossBranches(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun usage(value: Any): String {
    if (value is Service) {
        value.info()
    }
    value.info()
    return ""
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}

	var insideType, outsideType string
	var sawInside, sawOutside bool
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "value.info" {
			continue
		}
		line, _ := item["line_number"].(int)
		typ, _ := item["inferred_obj_type"].(string)
		switch line {
		case 9:
			insideType, sawInside = typ, true
		case 11:
			outsideType, sawOutside = typ, true
		}
	}
	if !sawInside || insideType != "Service" {
		t.Fatalf("guarded value.info inferred_obj_type = %q (seen=%v), want Service", insideType, sawInside)
	}
	if !sawOutside {
		t.Fatalf("post-block value.info call missing from %#v", items)
	}
	if outsideType != "" {
		t.Fatalf("post-block value.info inferred_obj_type = %q, want empty (no smart-cast leak)", outsideType)
	}
}

// TestDefaultEngineParsePathKotlinImportedTypeConstructorCallEmitsRow proves a
// regularly imported type (no `as` alias) is recognized as a constructor call
// target: `import com.acme.Widget` then `Widget()` emits a Widget call row. The
// pre-pass records the import's last path segment as a known type. Reported as a
// P2 on PR #3548.
func TestDefaultEngineParsePathKotlinImportedTypeConstructorCallEmitsRow(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "consumer.kt")
	writeTestFile(
		t,
		filePath,
		`package demo

import com.acme.Widget
import com.acme.helper

fun run(): String {
    val w = Widget()
    return helper(w)
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	// Imported type constructed with `Widget()` emits a constructor call.
	assertNamedBucketContains(t, got, "function_calls", "Widget")
	// Imported top-level function `helper()` still emits a bare call edge
	// (Kotlin imports do not distinguish a function from a type), preserving the
	// #3528 imported-bare-call behavior.
	assertNamedBucketContains(t, got, "function_calls", "helper")
}

// TestDefaultEngineParsePathKotlinSmartCastAppliesToUnbracedIfConsequent proves
// a concise (unbraced) guard `if (value is Service) value.info()` narrows the
// receiver type for the consequent call, yielding inferred_obj_type=Service.
// Reported as a P2 on PR #3548.
func TestDefaultEngineParsePathKotlinSmartCastAppliesToUnbracedIfConsequent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Usage.kt")
	writeTestFile(
		t,
		filePath,
		`package comprehensive

class Service {
    fun info(): String = "ok"
}

fun usage(value: Any): String {
    if (value is Service) value.info()
    return ""
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	items, ok := got["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", got["function_calls"])
	}
	for _, item := range items {
		fullName, _ := item["full_name"].(string)
		if fullName != "value.info" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Service")
		return
	}
	t.Fatalf("function_calls missing full_name=%q in %#v", "value.info", items)
}
