// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPInfersMethodReturnCallChainReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_return_call_chain.php")
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

class Config {
    private Factory $factory;

    public function run(string $message): void {
        $this->factory->createService()->info($message);
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

	infoCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$this->factory->createService().info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Service")
}

func TestDefaultEngineParsePathPHPInfersMethodReturnPropertyDereferenceReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_return_property_dereference.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class Logger {
    public function info(string $message): void {}
}

class Service {
    public Logger $logger;
}

class Factory {
    public function createService(): Service {
        return new Service();
    }
}

class Config {
    private Factory $factory;

    public function run(string $message): void {
        $this->factory->createService()->logger->info($message);
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

	loggerCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$this->factory->createService()->logger.info")
	parsertest.AssertStringFieldValue(t, loggerCall, "inferred_obj_type", "Logger")
}
