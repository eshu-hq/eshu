package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPHPInfersFreeFunctionReturnPropertyChainReceiverCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_return_property_chain_alias.php")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	factoryItem := assertBucketItemByName(t, got, "functions", "createFactory")
	phpAssertStringFieldValue(t, factoryItem, "return_type", "Factory")

	loggerItem := assertBucketItemByFieldValue(t, got, "variables", "name", "$logger")
	phpAssertStringFieldValue(t, loggerItem, "type", "Logger")

	infoCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "$logger.info")
	phpAssertStringFieldValue(t, infoCall, "inferred_obj_type", "Logger")
}
