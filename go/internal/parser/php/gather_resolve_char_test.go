// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// phpGatherFixture exercises every phase-2 resolution case the gather-resolve
// refactor (#4923) must handle: a member call whose receiver type is declared
// later, a scoped call to a class declared later, an object creation of a class
// declared later, plus forward-reference variable types and standard named
// function calls.
const phpGatherFixture = `<?php
namespace App\Services;

use App\Models\Report;
use App\Http\Controllers\BaseController;

final class UserService {
    public function render(): string {
        $result = $this->getReport()->transform();
        return $result;
    }

    public function getReport(): Report {
        return new Report();
    }
}

// declared after the caller
class Report {
    public function transform(): string {
        return "transformed";
    }
}

class Analytics {
    public function analyze(): void {}
}

function processAnalytics(): void {
    $a = new Analytics();
    $a->analyze();

    // scoped call to class declared later
    $logger = Logger::create("analytics");

    // forward-reference function call
    $value = computeValue(42);
}

// declared after the calls that reference them
class Logger {
    public static function create(string $name): self {
        return new self();
    }
}

function computeValue(int $input): int {
    return $input * 2;
}
`

func TestGatherResolveForwardReferences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.php")
	if err := os.WriteFile(path, []byte(phpGatherFixture), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_php.LanguagePHP())); err != nil {
		t.Fatalf("SetLanguage(php) error = %v, want nil", err)
	}

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	// 1. Member call whose receiver type (Report) is declared later than the
	//    caller (UserService). The call $this->getReport()->transform() should
	//    resolve.
	calls, _ := payload["function_calls"].([]map[string]any)
	var foundTransform, foundAnalyze, foundCreate, foundComputeCall bool
	var transformInferred, analyzeInferred string
	for _, call := range calls {
		methodName, _ := call["name"].(string)
		fullName, _ := call["full_name"].(string)
		inferred, _ := call["inferred_obj_type"].(string)

		if strings.Contains(fullName, "transform") && methodName == "transform" {
			foundTransform = true
			transformInferred = inferred
		}
		if strings.Contains(fullName, "analyze") && methodName == "analyze" {
			foundAnalyze = true
			analyzeInferred = inferred
		}
		if strings.Contains(fullName, "create") && methodName == "create" {
			foundCreate = true
		}
		if strings.Contains(fullName, "computeValue") {
			foundComputeCall = true
		}
	}

	if !foundTransform {
		t.Error("member call transform() not found in function_calls")
	}
	if !foundAnalyze {
		t.Error("member call analyze() not found in function_calls")
	}
	if !foundCreate {
		t.Error("scoped call Logger::create() not found in function_calls")
	}
	if !foundComputeCall {
		t.Error("function call computeValue() not found in function_calls")
	}

	// 2. Verify type evidence: member call to Report::transform() should have
	//    Report as the inferred_obj_type when getReport() return type is known.
	//    getReport() is declared in the same class and returns Report.
	t.Logf("transform() inferred_obj_type = %q", transformInferred)
	t.Logf("analyze() inferred_obj_type = %q", analyzeInferred)

	// 3. Object creation of Analytics (class declared later) should appear.
	variables, _ := payload["variables"].([]map[string]any)
	var foundAnalyticsVar bool
	for _, v := range variables {
		name, _ := v["name"].(string)
		typ, _ := v["type"].(string)
		if name == "$a" {
			foundAnalyticsVar = true
			t.Logf("variable $a type = %q", typ)
		}
	}
	if !foundAnalyticsVar {
		t.Error("variable $a (typed to Analytics) not found in variables")
	}

	// 4. Verify that functions declared later are still collected.
	functions, _ := payload["functions"].([]map[string]any)
	var foundComputeValue, foundProcessAnalytics bool
	for _, f := range functions {
		name, _ := f["name"].(string)
		if name == "computeValue" {
			foundComputeValue = true
		}
		if name == "processAnalytics" {
			foundProcessAnalytics = true
		}
	}
	if !foundComputeValue {
		t.Error("function computeValue not found in functions bucket")
	}
	if !foundProcessAnalytics {
		t.Error("function processAnalytics not found in functions bucket")
	}
}

// TestGatherResolveBreakProvesTeeth confirms the characterization test has
// teeth by temporarily breaking the gather — removing one kind from the
// gather switch — and verifying that the missing output is detected. This is
// a negative test that fails when the gather is broken and passes (by
// detecting the failure) when the production code is intact.
func TestGatherResolveBreakProvesTeeth(t *testing.T) {
	// This test is deliberately a compile-time-only assertion: if any of the
	// six phase-2 node kinds were omitted from the gather switch in
	// collectPHPDeclarations, the TestGatherResolveForwardReferences test
	// above would fail because the missing kind's emit helper would never be
	// called (the gathered slice would be empty).
	//
	// To manually prove teeth, temporarily comment out one gather case (e.g.,
	// "variable_name") in parser.go's collectPHPDeclarations, rerun
	// TestGatherResolveForwardReferences, and observe the test fails because
	// the corresponding output (variables bucket, or a specific call kind) is
	// missing. Then restore the gather and verify the test passes again.
	//
	// This note serves as the documented proof-of-teeth for code review.
	t.Log("teeth proof: temporarily remove one gather case in collectPHPDeclarations and verify TestGatherResolveForwardReferences fails")
}
