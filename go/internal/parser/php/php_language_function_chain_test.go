// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPInfersDirectFreeFunctionReturnReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_call_chain.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class Service {
    public function info(string $message): void {}
}

function createService(): Service {
    return new Service();
}

class Config {
    public function run(string $message): void {
        createService()->info($message);
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

	createService := parsertest.AssertBucketItemByName(t, got, "functions", "createService")
	parsertest.AssertStringFieldValue(t, createService, "return_type", "Service")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "createService().info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersFreeFunctionReturnCallChainReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_call_chain.php")
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
}

function createFactory(): Factory {
    return new Factory();
}

class Config {
    public function run(string $message): void {
        createFactory()->createService()->info($message);
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

	factoryCall := parsertest.AssertBucketItemByName(t, got, "functions", "createFactory")
	parsertest.AssertStringFieldValue(t, factoryCall, "return_type", "Factory")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "name", "info")
	phpAssertStringFieldContains(t, infoCall, "full_name", "createFactory")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}
