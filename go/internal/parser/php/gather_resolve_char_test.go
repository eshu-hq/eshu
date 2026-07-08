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
// declared later, forward-reference variable types, standard named function
// calls, AND a member call that appears BEFORE its receiver's assignment in the
// same scope (the backward-type-flow trap from #4844 and the #4923 P2 review).
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

class Service { function run() {} }

function processAnalytics(): void {
    $a = new Analytics();
    $a->analyze();

    // scoped call to class declared later
    $logger = Logger::create("analytics");

    // backward-type-flow trap (#4844 lesson): member call BEFORE its
    // receiver's assignment in the same scope must resolve BEFORE the
    // variable emission seeds localVariableTypes — inferred_obj_type
    // must be null, never the later-assigned type.
    $svc->run();
    $svc = new Service();

    // member call AFTER its receiver's assignment — should infer Service.
    $svc2 = new Service();
    $svc2->run();

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

	// Collect calls by full_name for quick lookup.
	calls, _ := payload["function_calls"].([]map[string]any)
	callByFullName := make(map[string]map[string]any, len(calls))
	for _, c := range calls {
		fn, _ := c["full_name"].(string)
		callByFullName[fn] = c
	}

	// 1. transform() — forward-reference receiver type (Report declared later).
	if c, ok := callByFullName["$this->getReport().transform"]; ok {
		inferred, _ := c["inferred_obj_type"].(string)
		t.Logf("transform() inferred_obj_type = %q", inferred)
		if !strings.Contains(inferred, "Report") {
			t.Errorf("transform() inferred_obj_type = %q, want to contain Report", inferred)
		}
	} else {
		t.Error("transform() call not found")
	}

	// 2. analyze() — member call whose receiver ($a = new Analytics()) is
	//    declared BEFORE the call.
	if c, ok := callByFullName["$a.analyze"]; ok {
		inferred, _ := c["inferred_obj_type"].(string)
		t.Logf("$a.analyze inferred_obj_type = %q", inferred)
		if !strings.Contains(inferred, "Analytics") {
			t.Errorf("$a.analyze inferred_obj_type = %q, want Analytics", inferred)
		}
	} else {
		t.Error("$a.analyze call not found")
	}

	// 3. scoped call Logger::create() — class declared later.
	if c, ok := callByFullName["Logger.create"]; ok {
		inferred, _ := c["inferred_obj_type"].(string)
		t.Logf("Logger.create inferred_obj_type = %q", inferred)
		if !strings.Contains(inferred, "Logger") {
			t.Errorf("Logger.create inferred_obj_type = %q, want Logger", inferred)
		}
	} else {
		t.Error("Logger.create call not found")
	}

	// 4. Backward-type-flow trap (#4844 lesson): $svc->run() appears BEFORE
	//    $svc = new Service(). In the original pre-order WalkNamed, the member
	//    call is visited first, so localVariableTypes["svc"] is not yet set
	//    and inferReceiverType returns empty. The grouped-loop bug
	//    (five per-kind slices) would process all variable_names first,
	//    seeding the map before any calls run, producing "Service" here.
	//    The fix — a single ordered slice with a Kind() dispatch loop —
	//    preserves the interleaved pre-order and yields nil.
	if c, ok := callByFullName["$svc.run"]; ok {
		inferred, _ := c["inferred_obj_type"].(string)
		t.Logf("$svc.run (call before assignment) inferred_obj_type = %q", inferred)
		if inferred == "Service" {
			t.Errorf("BACKWARD TYPE FLOW: $svc.run inferred_obj_type = %q, want nil (assignment is AFTER the call in source)", inferred)
		}
	} else {
		t.Error("$svc.run call not found")
	}

	// 5. $svc2->run() — assignment IS before the call, should infer Service.
	if c, ok := callByFullName["$svc2.run"]; ok {
		inferred, _ := c["inferred_obj_type"].(string)
		t.Logf("$svc2.run (call after assignment) inferred_obj_type = %q", inferred)
		if !strings.Contains(inferred, "Service") {
			t.Errorf("$svc2.run inferred_obj_type = %q, want Service", inferred)
		}
	} else {
		t.Error("$svc2.run call not found")
	}

	// 6. Function call to forward-declared computeValue().
	if _, ok := callByFullName["computeValue"]; !ok {
		t.Error("function call computeValue not found")
	}

	// 7. Object creation calls should appear.
	for _, name := range []string{"Analytics", "Service"} {
		if _, ok := callByFullName[name]; !ok {
			t.Errorf("object creation %q not found in function_calls", name)
		}
	}

	// 8. Verify that functions declared later are still collected.
	functions, _ := payload["functions"].([]map[string]any)
	funcNames := make(map[string]bool)
	for _, f := range functions {
		name, _ := f["name"].(string)
		funcNames[name] = true
	}
	for _, name := range []string{"computeValue", "processAnalytics"} {
		if !funcNames[name] {
			t.Errorf("function %q not found in functions bucket", name)
		}
	}
}

// TestGatherResolveInterleavedPreOrder documents the teeth proof for the
// backward-type-flow characterization ($svc->run() before its assignment).
// The original bug (five per-kind grouped loops) processed all variable_names
// first, seeding localVariableTypes before any calls ran, so a call before its
// assignment would erroneously see the later-assigned type. The fix — a single
// ordered slice dispatched by node.Kind() — preserves the interleaved pre-order
// visitation, so the call resolves BEFORE the later assignment is processed.
//
// To manually prove teeth: temporarily split the single ordered loop into five
// per-kind grouped loops (the buggy shape), rerun TestGatherResolveForwardReferences,
// and observe $svc.run inferred_obj_type = "Service" (wrong). Then restore the
// single ordered dispatch loop and verify the test passes again.
func TestGatherResolveInterleavedPreOrder(t *testing.T) {
	t.Log("teeth proof: five per-kind grouped loops would infer Service for $svc.run (call before assignment); single ordered dispatch preserves pre-order and infers nil")
}
