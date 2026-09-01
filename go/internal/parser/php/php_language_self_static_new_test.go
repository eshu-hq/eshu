// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPInfersSelfAndStaticInstantiationReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "self_static_instantiation.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

class Factory {
    public function createService(): Service {
        return new Service();
    }

    public function run(string $message): void {
        new self()->createService()->info($message);
        new static()->createService()->info($message);
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

	selfInfo := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "new self()->createService().info")
	parsertest.AssertStringFieldValue(t, selfInfo, "inferred_obj_type", "Service")

	staticInfo := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "new static()->createService().info")
	parsertest.AssertStringFieldValue(t, staticInfo, "inferred_obj_type", "Service")
}
