// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPHPInfersFreeFunctionReturnPropertyChainReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_return_property_chain_alias.php")
	parsertest.WriteFile(
		t,
		filePath,
		`<?php
class Logger {
    public function info(string $message): void {}
}

class Factory {
    public Logger $logger;
}

function createFactory(): Factory {
    return new Factory();
}

class Config {
    public function run(string $message): void {
        $logger = createFactory()->logger;
        $logger->info($message);
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

	factoryItem := parsertest.AssertBucketItemByName(t, got, "functions", "createFactory")
	parsertest.AssertStringFieldValue(t, factoryItem, "return_type", "Factory")

	loggerItem := parsertest.AssertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	parsertest.AssertStringFieldValue(t, loggerItem, "type", "Logger")

	infoCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	parsertest.AssertStringFieldValue(t, infoCall, "inferred_obj_type", "Logger")
}
