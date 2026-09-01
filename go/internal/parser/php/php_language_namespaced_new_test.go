// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestDefaultEngineParsePathPHPInfersNamespacedInstantiationReceiverCalls pins
// that `new Demo\Service()` instantiation infers the receiver type for both a
// variable-bound receiver and a directly chained parenthesized receiver. The
// inferred type is the last namespace segment (Service), matching how
// object-creation call rows normalize qualified type names. Single-identifier
// instantiation stays green via the bare Service call.
func TestDefaultEngineParsePathPHPInfersNamespacedInstantiationReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "namespaced_instantiation.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
namespace App;

class Runner {
    public function run(): void {}
}

class Caller {
    public function viaVariable(): void {
        $svc = new Demo\Service();
        $svc->run();
    }

    public function viaChain(): void {
        (new Demo\Service())->run();
    }

    public function viaBare(): void {
        $bare = new Service();
        $bare->run();
    }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	variableCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$svc.run")
	parsertest.AssertStringFieldValue(t, variableCall, "inferred_obj_type", "Service")

	chainCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "new Demo\\Service().run")
	parsertest.AssertStringFieldValue(t, chainCall, "inferred_obj_type", "Service")

	bareCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$bare.run")
	parsertest.AssertStringFieldValue(t, bareCall, "inferred_obj_type", "Service")
}
